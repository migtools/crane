# Crane Validate: Offline / Disconnected Cluster Metadata Support

**Date:** 2026-04-28
**Status:** Proposal
**Related PR:** [https://github.com/migtools/crane/pull/302](https://github.com/migtools/crane/pull/302) (live-cluster validate)
**Related Issue:** [https://github.com/migtools/crane/issues/319](https://github.com/migtools/crane/issues/319)

---

## Problem Statement

The existing `crane validate` command requires a live connection to the target Kubernetes cluster via kubeconfig. This doesn't cover real-world scenarios where:

1. **Air-gapped / disconnected environments** -- the target cluster is on a restricted network with no direct access from the migration workstation.
2. **Pre-provisioning planning** -- the target cluster doesn't exist yet; the team wants to validate against a *planned* cluster profile (e.g., "OpenShift 4.14" or "EKS 1.29").
3. **Different teams / handoffs** -- the source-cluster team exports manifests, but a different team (with cluster access) needs to share target cluster info without granting kubeconfig access.
4. **CI/CD pipelines** -- validation runs in a pipeline that has no network path to the target cluster.
5. **ArgoCD / GitOps** -- only ArgoCD holds credentials to the target cluster; developers and CI have no direct access.

We need a way to pass **target cluster metadata as a file** so `crane validate` can run the same GVK compatibility checks without a live connection.

---

## Current Architecture (Live Mode)

```
crane validate -i output/ --context target-cluster
```

**What the live validate does today:**

1. **ScanManifests** -- walks `--input-dir`, parses YAML/JSON, extracts distinct `(apiVersion, kind, namespace)` tuples.
2. **MatchResults** -- calls `discovery.ServerGroupsAndResources()` on the target cluster, builds a `map[groupVersion]map[kind]discoveryEntry` index, matches each manifest entry against it.
3. **Report + Failures** -- writes a JSON/YAML report and failure artifacts.

**The only cluster data consumed is the discovery index** -- i.e., the full list of `(group, version, kind, resourcePlural, namespaced)` tuples served by the API server. That's what we need to capture offline.

---

## Proposed Design

### 1. Cluster Metadata File Format

A single JSON or YAML file called a **Cluster Profile**. It contains exactly the information `crane validate` needs, structured to be both human-readable and machine-parseable.

#### Schema

```yaml
# cluster-profile.yaml
apiVersion: crane.konveyor.io/v1alpha1
kind: ClusterProfile

# Informational -- helps the user identify what this profile represents.
metadata:
  name: "ocp-414-prod"                          # user-chosen identifier
  createdAt: "2026-04-28T15:04:05Z"             # when this was captured
  kubernetesVersion: "1.27.8"                    # server version
  platform: "OpenShift"                          # optional: OpenShift, EKS, GKE, AKS, RKE, vanilla, etc.
  platformVersion: "4.14.12"                     # optional: platform-specific version

# The core data -- this is what validate actually matches against.
# Each entry is one API group-version with its served resources.
apiResources:
  - groupVersion: "v1"
    resources:
      - kind: "Pod"
        name: "pods"                             # plural resource name
        namespaced: true
      - kind: "Service"
        name: "services"
        namespaced: true
      - kind: "Namespace"
        name: "namespaces"
        namespaced: false
      # ... all core resources
  - groupVersion: "apps/v1"
    resources:
      - kind: "Deployment"
        name: "deployments"
        namespaced: true
      - kind: "StatefulSet"
        name: "statefulsets"
        namespaced: true
      # ...
  - groupVersion: "route.openshift.io/v1"
    resources:
      - kind: "Route"
        name: "routes"
        namespaced: true
      # ...
```

#### Why This Format


| Decision                                            | Rationale                                                                                                |
| --------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| Custom `crane.konveyor.io/v1alpha1` type            | Follows Kubernetes API conventions; versionable; distinguishable from raw K8s objects                    |
| `apiResources` as top-level array of group-versions | Maps 1:1 to what `ServerGroupsAndResources()` returns -- easy to build the same `map[gv]map[kind]` index |
| `kind` + `name` (plural) per resource               | `kind` is what we match; `name` (plural) is what we report in results (e.g., `ResourcePlural` field)     |
| `namespaced` flag per resource                      | Enables future validation (e.g., "this cluster-scoped resource is being placed in a namespace")          |
| `metadata` block is informational only              | Not used for matching -- purely for humans and audit trails                                              |
| JSON and YAML both accepted                         | Crane already supports both; `kubectl` output is typically JSON                                          |


#### Minimal vs. Full Metadata

The **minimal required fields** for matching are:

```yaml
apiResources:
  - groupVersion: "apps/v1"
    resources:
      - kind: "Deployment"
```

Everything else (`name`, `namespaced`, `metadata.*`) is optional but recommended. The validate command should work with just `groupVersion` + `kind` entries, logging a warning if `name` is missing (the `ResourcePlural` field in the report will be empty).

---

### 2. How Users Retrieve / Generate the Profile

The cluster metadata is always captured using standard `kubectl` commands -- no crane installation is needed on the target cluster side. The `crane cluster-profile` command converts the raw kubectl output into the ClusterProfile format.

#### Step 1: Capture with kubectl (on the target cluster)

```bash
# Run by anyone with kubectl access to the target cluster (no crane needed)
kubectl api-resources -o wide --no-headers > api-resources.txt
kubectl version -o json > version.json (optional)
```

#### Step 2: Convert with crane (on the migration workstation)

```bash
crane cluster-profile \
  --api-resources api-resources.txt \
  --version version.json \
  -o cluster-profile.yaml
```

The `kubectl api-resources -o wide` output contains all needed columns:

```
NAME          SHORTNAMES   APIVERSION   NAMESPACED   KIND          VERBS
deployments   deploy       apps/v1      true         Deployment    [create delete ...]
```

**Parser maps:** `APIVERSION` -> `groupVersion`, `KIND` -> `kind`, `NAME` -> `name` (plural), `NAMESPACED` -> `namespaced`.

The `kubectl version -o json` output provides the server version for the `metadata.kubernetesVersion` field. This input is optional -- if omitted, the field is left empty.

---

### 3. CLI Integration

#### New flag on `crane validate`

```bash
crane validate -i output/ --cluster-profile cluster-profile.yaml
```


| Flag                | Type     | Description                                                                                |
| ------------------- | -------- | ------------------------------------------------------------------------------------------ |
| `--cluster-profile` | `string` | Path to a ClusterProfile YAML/JSON file. Mutually exclusive with kubeconfig/context flags. |


**Mutual exclusion logic:**

```
if --cluster-profile is set AND (--kubeconfig or --context is set):
    error: "--cluster-profile and --kubeconfig/--context are mutually exclusive; 
           use --cluster-profile for offline validation or kubeconfig flags for live validation"

if neither --cluster-profile NOR kubeconfig/context is set:
    error: "must specify either --cluster-profile for offline validation or 
           kubeconfig flags for live cluster validation"
```

#### New `crane cluster-profile` command

A single command (no subcommands) that converts `kubectl` output into a ClusterProfile file.

```bash
crane cluster-profile \
  --api-resources api-resources.txt \
  --version version.json \       # optional
  --name "ocp-414-prod" \        # optional: identifier for the profile
  -o cluster-profile.yaml
```


| Flag              | Type     | Required | Description                                                               |
| ----------------- | -------- | -------- | ------------------------------------------------------------------------- |
| `--api-resources` | `string` | Yes      | Path to `kubectl api-resources -o wide --no-headers` output               |
| `--version`       | `string` | No       | Path to `kubectl version -o json` output (for kubernetesVersion metadata) |
| `--name`          | `string` | No       | Identifier for the profile (defaults to filename without extension)       |
| `-o, --output`    | `string` | No       | Output file path (defaults to stdout)                                     |


---

### 4. Internal Architecture Changes

#### Current flow (live):

```
cmd/validate
  -> ScanManifests(dirs)           -> []ManifestEntry
  -> configFlags.ToDiscoveryClient()
  -> MatchResults(entries, MatchOptions{DiscoveryClient: client})
     -> buildDiscoveryIndex(client) -> map[gv]map[kind]discoveryEntry
     -> matchEntry / addSuggestion
  -> FormatTable / FormatJSON / FormatYAML
```

#### Proposed flow (offline + live):

```
cmd/validate
  -> ScanManifests(dirs)           -> []ManifestEntry
  -> if --cluster-profile:
       LoadClusterProfile(path)    -> *ClusterProfile
       BuildIndexFromProfile(cp)   -> map[gv]map[kind]discoveryEntry
     else:
       configFlags.ToDiscoveryClient()
       buildDiscoveryIndex(client) -> map[gv]map[kind]discoveryEntry
  -> MatchResultsFromIndex(entries, index)   // new: takes index directly
     -> matchEntry / addSuggestion           // unchanged
  -> FormatTable / FormatJSON / FormatYAML   // unchanged
```

**Key refactoring: decouple index building from matching.**

Currently `MatchResults` takes a `DiscoveryClient` and builds the index internally. We need to:

1. Extract `buildDiscoveryIndex` result type as a public `DiscoveryIndex` (or just `map[string]map[string]discoveryEntry`).
2. Add a `BuildIndexFromProfile(*ClusterProfile)` function that produces the same index type.
3. Create `MatchResultsFromIndex(entries, index)` that takes a pre-built index.
4. Keep `MatchResults` as a convenience wrapper (calls `buildDiscoveryIndex` then `MatchResultsFromIndex`).

This is a small, clean refactor -- the matching logic (`matchEntry`, `addSuggestion`, `buildKindIndex`) stays untouched.

#### New types

```go
// internal/validate/profile.go

// ClusterProfile is the on-disk representation of a target cluster's API surface.
type ClusterProfile struct {
    APIVersion string                `json:"apiVersion" yaml:"apiVersion"`
    Kind       string                `json:"kind" yaml:"kind"`
    Metadata   ClusterProfileMeta    `json:"metadata" yaml:"metadata"`
    Resources  []GroupVersionResources `json:"apiResources" yaml:"apiResources"`
}

type ClusterProfileMeta struct {
    Name              string `json:"name" yaml:"name"`
    CreatedAt         string `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
    KubernetesVersion string `json:"kubernetesVersion,omitempty" yaml:"kubernetesVersion,omitempty"`
    Platform          string `json:"platform,omitempty" yaml:"platform,omitempty"`
    PlatformVersion   string `json:"platformVersion,omitempty" yaml:"platformVersion,omitempty"`
}

type GroupVersionResources struct {
    GroupVersion string           `json:"groupVersion" yaml:"groupVersion"`
    Resources    []ProfileResource `json:"resources" yaml:"resources"`
}

type ProfileResource struct {
    Kind       string `json:"kind" yaml:"kind"`
    Name       string `json:"name,omitempty" yaml:"name,omitempty"`       // plural
    Namespaced bool   `json:"namespaced,omitempty" yaml:"namespaced,omitempty"`
}

// LoadClusterProfile reads and validates a ClusterProfile from a YAML or JSON file.
func LoadClusterProfile(path string) (*ClusterProfile, error) { ... }

// BuildIndexFromProfile converts a ClusterProfile into the same discovery index
// format used by the live-cluster code path.
func BuildIndexFromProfile(cp *ClusterProfile) map[string]map[string]discoveryEntry { ... }

// ParseKubectlAPIResources parses the output of `kubectl api-resources -o wide --no-headers`
// and returns a ClusterProfile.
func ParseKubectlAPIResources(apiResourcesData []byte, versionData []byte, name string) (*ClusterProfile, error) { ... }
```

#### New command files

```
cmd/
  cluster-profile/
    cluster_profile.go   -- crane cluster-profile (kubectl output -> ClusterProfile)
internal/
  validate/
    profile.go           -- ClusterProfile types, Load, BuildIndex, ParseKubectlAPIResources
    profile_test.go      -- unit tests
```

---

### 5. Validation Report Changes

The report should indicate **which mode** was used:

```json
{
  "mode": "offline",
  "clusterProfile": "ocp-414-prod",
  "clusterProfileSource": "cluster-profile.yaml",
  "kubernetesVersion": "1.27.8",
  "platform": "OpenShift 4.14.12",
  "results": [ ... ],
  "totalScanned": 42,
  "compatible": 40,
  "incompatible": 2
}
```

For live mode, these fields would be:

```json
{
  "mode": "live",
  "clusterContext": "target-cluster",
  "kubernetesVersion": "1.27.8",
  ...
}
```

This gives operators an audit trail of *what* the manifests were validated against.

---

### 6. Implementation Phases

#### Phase 1: Core Offline Validate + cluster-profile command (MVP)

**Goal:** `crane cluster-profile` converts kubectl output, and `crane validate --cluster-profile file.yaml` works end-to-end.


| Task                                                                   | Files                                     | Effort |
| ---------------------------------------------------------------------- | ----------------------------------------- | ------ |
| Define `ClusterProfile` types                                          | `internal/validate/profile.go`            | S      |
| `LoadClusterProfile` -- read + validate file                           | `internal/validate/profile.go`            | S      |
| `ParseKubectlAPIResources` -- parse kubectl output into ClusterProfile | `internal/validate/profile.go`            | M      |
| `BuildIndexFromProfile` -- convert to discovery index                  | `internal/validate/profile.go`            | S      |
| Refactor `MatchResults` to accept pre-built index                      | `internal/validate/matcher.go`            | S      |
| Add `--cluster-profile` flag + mutual exclusion                        | `cmd/validate/validate.go`                | S      |
| `crane cluster-profile` command (kubectl -> ClusterProfile)            | `cmd/cluster-profile/cluster_profile.go`  | M      |
| Add mode/profile info to `ValidationReport`                            | `internal/validate/types.go`, `report.go` | S      |
| Unit tests for kubectl parsing                                         | `internal/validate/profile_test.go`       | M      |
| Unit tests for profile loading and index building                      | `internal/validate/profile_test.go`       | M      |
| Unit tests for offline matching                                        | `internal/validate/matcher_test.go`       | S      |
| Update validate command tests                                          | `cmd/validate/validate_test.go`           | S      |


**Estimated effort:** ~3-4 days

---

### 7. User Workflows

#### Workflow 1: Air-gapped migration

```bash
# Person A: has access to target cluster (air-gapped network, only kubectl)
kubectl api-resources -o wide --no-headers > api-resources.txt
kubectl version -o json > version.json
# Transfer files out via approved channel (USB, bastion, etc.)

# Person B: has the exported manifests (migration workstation)
crane cluster-profile \
  --api-resources api-resources.txt \
  --version version.json \
  -o target-profile.yaml

crane export ...
crane transform ...
crane apply ...
crane validate -i output/ --cluster-profile target-profile.yaml
```

#### Workflow 2: Pre-provisioning check

```bash
# Architect: "Will our EKS manifests work on OpenShift 4.14?"
# Someone with access to a reference OCP 4.14 cluster runs:
kubectl api-resources -o wide --no-headers > ocp-414-api-resources.txt
kubectl version -o json > ocp-414-version.json

# Migration team converts and validates:
crane cluster-profile \
  --api-resources ocp-414-api-resources.txt \
  --version ocp-414-version.json \
  --name ocp-414 \
  -o ocp-414.yaml

crane validate -i output/ --cluster-profile ocp-414.yaml
# Report shows: Route kind not in source, Ingress available as networking.k8s.io/v1, etc.
```

#### Workflow 3: ArgoCD / GitOps pipeline

In an ArgoCD-managed environment the typical challenge is that **only ArgoCD holds credentials to the target cluster** -- the developer writing manifests and the CI pipeline running validation have no direct `kubectl` access.

**Who captures the cluster metadata:** The cluster admin or platform team that registered the cluster with ArgoCD. They already have `kubectl` access (they had to in order to install the ArgoCD agent or register the cluster secret). **They do NOT need crane installed** -- they just run two standard `kubectl` commands and hand over the raw output. The migration team converts it to a ClusterProfile using `crane cluster-profile`.

This is important because in ArgoCD setups, the platform team and the migration team are often different groups. The platform team manages clusters; they shouldn't need to install migration-specific tooling. Asking them to run `kubectl api-resources` is a trivial ask; asking them to install crane is friction that slows the whole process down.

**Where the profile lives:** Checked into the GitOps repo alongside the ArgoCD `Application` manifest, so it travels with the deployment definition.

```
gitops-repo/
  apps/
    my-app/
      application.yaml          # ArgoCD Application CR
      cluster-profiles/
        target-prod.yaml         # ClusterProfile (converted from kubectl output)
  manifests/
    my-app/
      deployment.yaml
      service.yaml
      route.yaml
```

**CI validates on every PR before ArgoCD ever syncs:**

```yaml
# .github/workflows/validate.yaml (or Tekton, GitLab CI, etc.)
jobs:
  validate-manifests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Run crane validate (offline -- no cluster access needed)
        run: |
          crane validate \
            -i manifests/my-app/ \
            --cluster-profile apps/my-app/cluster-profiles/target-prod.yaml \
            -o json \
            --validate-dir results/

      - name: Fail PR if incompatible resources found
        if: failure()
        run: echo "::error::Manifests contain resources incompatible with the target cluster"

      - uses: actions/upload-artifact@v4
        with:
          name: validation-report
          path: results/
```

**End-to-end ArgoCD flow:**

```bash
# STEP 1a (Platform team -- only kubectl needed, no crane)
# The admin runs two standard kubectl commands on the target cluster
kubectl api-resources -o wide --no-headers > api-resources.txt
kubectl version -o json > version.json
# Hands the files to the migration team (Slack, email, shared drive, etc.)

# STEP 1b (Migration team -- converts to ClusterProfile using crane)
crane cluster-profile \
  --api-resources api-resources.txt \
  --version version.json \
  --name target-prod \
  -o apps/my-app/cluster-profiles/target-prod.yaml
git add apps/my-app/cluster-profiles/target-prod.yaml
git commit -m "add cluster profile for target-prod (OCP 4.14, K8s 1.27)"
git push

# STEP 2 (Developer -- every change)
# Developer modifies manifests in a feature branch
vim manifests/my-app/deployment.yaml
git add . && git commit -m "update deployment image"
git push  # triggers CI

# STEP 3 (CI -- automatic)
# CI runs crane validate --cluster-profile (see CI workflow above)
# If manifests use an API not available on target-prod, PR is blocked

# STEP 4 (ArgoCD -- after PR merges)
# ArgoCD sees the merged change and syncs to the cluster
# By this point, GVK compatibility has already been validated
```

**Keeping the profile fresh:**


| Trigger                                   | Action                                                                     |
| ----------------------------------------- | -------------------------------------------------------------------------- |
| Cluster K8s version upgrade               | Platform team re-runs the two kubectl commands, migration team re-converts |
| New CRDs installed (e.g., Istio, Knative) | Same -- re-run and re-convert                                              |
| Scheduled staleness check                 | CI warns if `metadata.createdAt` is >30 days old                           |


This can be automated with a CronJob on the target cluster that runs `kubectl api-resources -o wide --no-headers` and `kubectl version -o json`, then commits the raw output to the GitOps repo. The CI pipeline or a post-commit hook converts to ClusterProfile before running validate.

**Why not pull the profile from ArgoCD's API directly?**

ArgoCD's API (`argocd cluster get <name>`) exposes server version and connection state, but **not** the full api-resources discovery data. ArgoCD doesn't cache or expose the equivalent of `ServerGroupsAndResources()`. So there's no shortcut through ArgoCD -- the metadata must be captured via `kubectl` by someone with direct cluster access. If ArgoCD adds discovery data to their cluster API in the future, a dedicated integration could be considered, but that's not viable today.

#### Workflow 4: CI/CD pipeline (generic, no cluster access)

```yaml
# .github/workflows/validate.yaml
jobs:
  validate:
    steps:
      - uses: actions/checkout@v4
      - name: Validate manifests
        run: |
          crane validate \
            -i manifests/ \
            --cluster-profile .crane/target-profile.yaml \
            -o json \
            --validate-dir results/
      - uses: actions/upload-artifact@v4
        with:
          name: validation-report
          path: results/
```

---

### 8. Edge Cases and Considerations


| Concern                                | Handling                                                                                                                                                                                                                     |
| -------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Stale profile**                      | Print a warning if `metadata.createdAt` is >30 days old: "Warning: cluster profile was captured 45 days ago; API surface may have changed"                                                                                   |
| **CRDs installed after capture**       | Document that the profile is a point-in-time snapshot. User should re-capture after CRD changes.                                                                                                                             |
| **Partial profile**                    | If a profile has zero `apiResources`, error out. If some entries lack `name`, warn but continue (ResourcePlural will be empty in report).                                                                                    |
| **Format detection**                   | Use file extension (`.yaml`/`.yml` vs `.json`) and content sniffing (leading `{` = JSON).                                                                                                                                    |
| **Verbs / RBAC**                       | Out of scope. Future enhancement could validate that the service account has permission to create the resources.                                                                                                             |
| **Schema version**                     | `apiVersion: crane.konveyor.io/v1alpha1` allows future schema evolution. Validate this field on load; reject unknown versions with a clear error.                                                                            |
| **Profile from different K8s version** | Informational only. A profile from K8s 1.25 is valid for checking against; the kubernetesVersion is just metadata.                                                                                                           |
| **ArgoCD multi-cluster**               | Each target cluster needs its own profile. The `--cluster-profile` flag takes one file. For multi-cluster ArgoCD setups, run validate once per target with the corresponding profile. CI matrix builds can parallelize this. |
| **ArgoCD ApplicationSet**              | When ApplicationSets deploy to N clusters, validate against each cluster's profile. A CI script can loop over `cluster-profiles/*.yaml` and run validate for each.                                                           |
| **kubectl output locale**              | `kubectl api-resources` uses fixed column headers regardless of locale. The parser matches on column positions from the header line, not hardcoded offsets.                                                                  |


---

### 9. Testing Strategy

- **Unit tests:** kubectl output parsing (valid, malformed, missing columns, extra whitespace), profile loading (valid, invalid, partial, JSON, YAML), index building from profile, matching against profile-built index (same cases as live matcher tests).
- **Integration tests:** Capture kubectl output from a test cluster, convert with `crane cluster-profile`, then `crane validate --cluster-profile` with the result -- ensures round-trip fidelity.
- **E2E tests:** Capture kubectl output from source cluster, validate exported manifests against it (should show incompatibilities for cross-platform scenarios).
- **Regression:** Ensure live-mode validate behavior is unchanged (no breaking changes).

---

### 10. Summary


| What                       | How                                                                                               |
| -------------------------- | ------------------------------------------------------------------------------------------------- |
| **Metadata needed**        | List of `(groupVersion, kind, pluralName, namespaced)` tuples -- same as K8s discovery API output |
| **Format**                 | `ClusterProfile` YAML/JSON with `crane.konveyor.io/v1alpha1` schema                               |
| **User retrieves it**      | `kubectl api-resources -o wide --no-headers` + `kubectl version -o json` on the target cluster    |
| **User converts it**       | `crane cluster-profile --api-resources <file> --version <file> -o profile.yaml`                   |
| **User validates with it** | `crane validate -i output/ --cluster-profile profile.yaml`                                        |
| **Internal change**        | Decouple index-building from matching; add profile loader that produces the same index type       |
| **Scope of matching**      | Identical to live mode -- strict GVK compatibility with suggestions                               |


