# Crane Validate: Offline / Disconnected Cluster Support (Plan B)

**Date:** 2026-04-28
**Status:** Proposal
**Related PR:** [https://github.com/migtools/crane/pull/302](https://github.com/migtools/crane/pull/302) (live-cluster validate)
**Related Issue:** [https://github.com/migtools/crane/issues/319](https://github.com/migtools/crane/issues/319)

---

## Problem Statement

The existing `crane validate` command requires a live connection to the target Kubernetes cluster via kubeconfig. This doesn't cover real-world scenarios where:

1. **Air-gapped / disconnected environments** -- the target cluster is on a restricted network with no direct access from the migration workstation.
2. **Pre-provisioning planning** -- the target cluster doesn't exist yet; the team wants to validate against a known cluster's API surface.
3. **Different teams / handoffs** -- the source-cluster team exports manifests, but a different team (with cluster access) needs to share target cluster info without granting kubeconfig access.
4. **CI/CD pipelines** -- validation runs in a pipeline that has no network path to the target cluster.

**Note:** This feature applies when a target cluster is known (existing or planned). When crane outputs manifests into a GitOps repo where the target cluster is not yet decided, GVK validation is not applicable -- crane's job stops at producing valid Kubernetes YAML.

---

## Approach

No custom format. No extra commands. Just accept the JSON output of `kubectl api-resources -o json` directly in `crane validate` via a new `--api-resources` flag. The user captures one file with kubectl, passes it to crane. Done.

The JSON output is a native Kubernetes `APIResourceList` -- the same type the Go discovery client returns internally. Zero custom parsing needed; just `json.Unmarshal` into `metav1.APIResourceList`.

---

## Current Architecture (Live Mode)

```
crane validate -i output/ --context target-cluster
```

1. **ScanManifests** -- walks `--input-dir`, extracts distinct `(apiVersion, kind, namespace)` tuples.
2. **MatchResults** -- calls `discovery.ServerGroupsAndResources()` on the target cluster, builds a `map[groupVersion]map[kind]discoveryEntry` index, matches each manifest entry.
3. **Report + Failures** -- writes a JSON/YAML report and failure artifacts.

The only cluster data consumed is the discovery index -- the list of `(groupVersion, kind, resourcePlural, namespaced)` tuples. That maps exactly to what `kubectl api-resources -o json` outputs.

---

## Proposed Design

### 1. User Captures API Resources with kubectl

On a machine with target cluster access (no crane needed):

```bash
kubectl api-resources -o json > api-resources.json
```

Output is a native Kubernetes `APIResourceList`:

```json
{
  "kind": "APIResourceList",
  "apiVersion": "v1",
  "groupVersion": "",
  "resources": [
    {
      "name": "deployments",
      "singularName": "deployment",
      "namespaced": true,
      "group": "apps",
      "version": "v1",
      "kind": "Deployment",
      "verbs": ["create", "delete", "get", "list", "patch", "update", "watch"],
      "shortNames": ["deploy"],
      "categories": ["all"]
    },
    {
      "name": "services",
      "namespaced": true,
      "version": "v1",
      "kind": "Service",
      "verbs": ["create", "delete", "get", "list", "patch", "update", "watch"],
      "shortNames": ["svc"],
      "categories": ["all"]
    },
    {
      "name": "routes",
      "namespaced": true,
      "group": "route.openshift.io",
      "version": "v1",
      "kind": "Route",
      "verbs": ["create", "delete", "get", "list", "patch", "update", "watch"]
    }
  ]
}
```

This gives us everything we need:

| JSON field | Maps to | Used for |
|------------|---------|----------|
| `group` + `version` | `groupVersion` (e.g., `apps/v1`; core resources omit `group`, so groupVersion = `v1`) | GVK matching |
| `kind` | `kind` | GVK matching |
| `name` | `resourcePlural` | Report output |
| `namespaced` | `namespaced` | Future: scope validation |

**Why JSON over tabular text:**
- `kubectl api-resources -o json` outputs a native `APIResourceList` -- the same Go type (`metav1.APIResourceList`) the discovery client returns
- Can deserialize directly with `json.Unmarshal` -- zero custom parsing logic
- No column alignment ambiguity, no empty SHORTNAMES gaps, no comma-separated field confusion
- `group` and `version` are separate fields -- no string splitting needed
- Subresources are not included in the output (matches live matcher behavior)
- Stable, machine-readable format across all K8s versions

### 2. New Flag on `crane validate`

```bash
crane validate -i output/ --api-resources api-resources.json
```

| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--api-resources` | `string` | No | Path to `kubectl api-resources -o json` output file. Mutually exclusive with `--context`/`--kubeconfig`. |

**Mutual exclusion logic:**

```
if --api-resources is set AND (--kubeconfig or --context is explicitly set):
    error: "--api-resources and --kubeconfig/--context are mutually exclusive;
           use --api-resources for offline validation or kubeconfig flags for live validation"
```

When `--api-resources` is NOT set, existing behavior is unchanged -- `crane validate` uses the default kubeconfig as it does today. No breaking changes.

That's it. No new commands. No new file formats. One flag.

---

### 3. Internal Architecture Changes

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
  -> if --api-resources:
       ParseAPIResourcesJSON(path) -> map[gv]map[kind]discoveryEntry
     else:
       configFlags.ToDiscoveryClient()
       buildDiscoveryIndex(client) -> map[gv]map[kind]discoveryEntry
  -> MatchResultsFromIndex(entries, index)   // new: takes index directly
     -> matchEntry / addSuggestion           // unchanged
  -> FormatTable / FormatJSON / FormatYAML   // unchanged
```

**Key refactoring: decouple index building from matching.**

Currently `MatchResults` takes a `DiscoveryClient` and builds the index internally. We need to:

1. Extract `buildDiscoveryIndex` result type as a public type (or just `map[string]map[string]discoveryEntry`).
2. Add `ParseAPIResourcesJSON(path)` that deserializes the kubectl JSON output and builds the same index type.
3. Create `MatchResultsFromIndex(entries, index)` that takes a pre-built index.
4. Keep `MatchResults` as a convenience wrapper (calls `buildDiscoveryIndex` then `MatchResultsFromIndex`).

Matching logic (`matchEntry`, `addSuggestion`, `buildKindIndex`) stays untouched.

#### New / modified files

```
cmd/validate/validate.go             -- add --api-resources flag + mutual exclusion
internal/validate/api_resources.go   -- ParseAPIResourcesJSON
internal/validate/api_resources_test.go
internal/validate/matcher.go         -- refactor: extract MatchResultsFromIndex
internal/validate/matcher_test.go    -- add offline matching tests
internal/validate/types.go           -- add mode info to ValidationReport
internal/validate/report.go          -- include api-resources source in report
```

No new command directories. No new types beyond what's needed for deserialization.

#### Parsing logic

```go
// internal/validate/api_resources.go

// ParseAPIResourcesJSON reads the JSON output of `kubectl api-resources -o json`
// and builds the same discovery index used by the live-cluster code path.
func ParseAPIResourcesJSON(path string) (map[string]map[string]discoveryEntry, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("reading api-resources file %q: %w", path, err)
    }

    var resourceList APIResourceListJSON
    if err := json.Unmarshal(data, &resourceList); err != nil {
        return nil, fmt.Errorf("parsing api-resources JSON: %w", err)
    }

    if len(resourceList.Resources) == 0 {
        return nil, fmt.Errorf("api-resources file %q contains no resources", path)
    }

    index := map[string]map[string]discoveryEntry{}
    for _, res := range resourceList.Resources {
        // Build groupVersion: "apps/v1" for non-core, "v1" for core
        gv := res.Version
        if res.Group != "" {
            gv = res.Group + "/" + res.Version
        }

        if _, ok := index[gv]; !ok {
            index[gv] = map[string]discoveryEntry{}
        }
        index[gv][res.Kind] = discoveryEntry{
            Resource: metav1.APIResource{
                Name:       res.Name,
                Kind:       res.Kind,
                Namespaced: res.Namespaced,
            },
        }
    }
    return index, nil
}

// APIResourceListJSON matches the JSON structure of `kubectl api-resources -o json`.
// Each resource has group and version as separate fields.
type APIResourceListJSON struct {
    Kind       string                `json:"kind"`
    APIVersion string                `json:"apiVersion"`
    Resources  []APIResourceEntryJSON `json:"resources"`
}

type APIResourceEntryJSON struct {
    Name       string   `json:"name"`
    Namespaced bool     `json:"namespaced"`
    Group      string   `json:"group,omitempty"`   // empty for core (v1) resources
    Version    string   `json:"version"`
    Kind       string   `json:"kind"`
    Verbs      []string `json:"verbs,omitempty"`   // not used, but present in output
}
```

No heuristics. No column detection. Just `json.Unmarshal`.

---

### 4. Validation Report Changes

Add the source mode to the report so users know what was validated against:

```json
{
  "mode": "offline",
  "apiResourcesSource": "api-resources.json",
  "results": [ ... ],
  "totalScanned": 42,
  "compatible": 40,
  "incompatible": 2
}
```

For live mode:

```json
{
  "mode": "live",
  "clusterContext": "target-cluster",
  "results": [ ... ],
  ...
}
```

---

### 5. Implementation Plan

Single phase -- everything ships together.

| Task | Files | Effort |
|------|-------|--------|
| `ParseAPIResourcesJSON` -- deserialize kubectl JSON into discovery index | `internal/validate/api_resources.go` | S |
| Refactor `MatchResults` to extract `MatchResultsFromIndex` | `internal/validate/matcher.go` | S |
| Add `--api-resources` flag + mutual exclusion logic | `cmd/validate/validate.go` | S |
| Add mode info to `ValidationReport` | `internal/validate/types.go`, `report.go` | S |
| Unit tests for JSON parsing (valid, empty, malformed, core vs non-core groups) | `internal/validate/api_resources_test.go` | M |
| Unit tests for offline matching | `internal/validate/matcher_test.go` | S |
| Update validate command tests | `cmd/validate/validate_test.go` | S |

**Estimated effort:** ~2 days

---

### 6. User Workflows

#### Workflow 1: Air-gapped migration

```bash
# Person A: has kubectl access to target cluster (air-gapped network)
kubectl api-resources -o json > api-resources.json
# Transfer file out via approved channel (USB, bastion, etc.)

# Person B: has the exported manifests (migration workstation)
crane export ...
crane transform ...
crane apply ...
crane validate -i output/ --api-resources api-resources.json
```

#### Workflow 2: Pre-provisioning check

```bash
# Someone with access to a reference OCP 4.14 cluster:
kubectl api-resources -o json > ocp-414-api-resources.json

# Migration team validates:
crane validate -i output/ --api-resources ocp-414-api-resources.json
```

#### Workflow 3: CI/CD pipeline (including GitOps)

```yaml
# .github/workflows/validate.yaml
jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Validate manifests against target cluster
        run: crane validate -i manifests/ --api-resources .crane/api-resources.json -o json --validate-dir results/
      - uses: actions/upload-artifact@v4
        with:
          name: validation-report
          path: results/
```

This works equally well in GitOps setups (ArgoCD, Flux, etc.) where the CI pipeline validates manifests before they're merged and synced.

For multi-cluster targeting, run validate once per cluster:

```yaml
strategy:
  matrix:
    target: [prod-east, prod-west, staging]
steps:
  - run: crane validate -i manifests/ --api-resources clusters/${{ matrix.target }}/api-resources.json
```

#### Scope boundary: GitOps without a known target cluster

Crane's two output scenarios:

1. **Crane outputs manifests for a known target cluster** (migration between clusters) -- validate makes sense, both live and offline. The target cluster exists or is planned, and its API surface can be captured.

2. **Crane outputs manifests into a GitOps repo where the target cluster is not yet known** -- the GitOps tool (ArgoCD, Flux) may not yet know which cluster the manifests will deploy to. In this case, **there is nothing to validate GVKs against**. Crane's job stops at producing valid Kubernetes YAML. GVK validation happens later, when a target cluster is assigned and its API resources can be captured.

`crane validate --api-resources` only applies to scenario 1. It requires a target cluster's API surface to exist (as a live connection or a captured file). When the target is purely a GitOps repo with no cluster decided yet, offline validation is not applicable.

---

### 7. Edge Cases and Considerations

| Concern | Handling |
|---------|----------|
| **Stale data** | Document that the file is a point-in-time snapshot. Users should re-capture after cluster upgrades or CRD changes. |
| **CRDs installed after capture** | Same as above -- re-run the kubectl command. |
| **Empty resources array** | Error: "api-resources file contains no resources" |
| **Malformed JSON** | Error from `json.Unmarshal` with clear message. |
| **Missing `kind` field at top level** | Warn if `kind` is not `APIResourceList`, but still attempt to parse. |
| **Core resources (no `group` field)** | `group` is omitted for core API resources. Parser uses `version` alone as `groupVersion` (e.g., `"v1"`). |
| **Duplicate resources across groups** | Multiple entries with the same kind but different group/version are valid (e.g., `Event` in `v1` and `events.k8s.io/v1`). Index handles this naturally since it's keyed by `groupVersion`. |
| **Verbs / RBAC** | Out of scope. `verbs` field is present in JSON but ignored. Future enhancement could validate create/update permissions. |
| **Multi-cluster** | `--api-resources` takes one file. For N clusters, run validate N times with the corresponding file. |
| **kubectl version compatibility** | `kubectl api-resources -o json` is available since K8s 1.11+. The `APIResourceList` schema is stable. |
| **GitOps without target cluster** | When crane outputs to a GitOps repo and no target cluster is known yet, offline validate is not applicable. GVK validation requires a target cluster's API surface. |

---

### 8. Testing Strategy

- **Unit tests:** JSON parsing -- valid output, empty resources, malformed JSON, core resources (no group), non-core resources (with group), duplicate kinds across groups.
- **Unit tests:** Offline matching -- same scenarios as live matcher tests but with a parsed index instead of discovery client.
- **Integration tests:** Capture `kubectl api-resources -o json` from a test cluster, run `crane validate --api-resources` with it -- ensure results match live validate against the same cluster.
- **E2E tests:** Cross-platform scenarios (EKS api-resources validated against OCP manifests).
- **Regression:** Live-mode validate behavior unchanged.

---

### 9. Summary

| What | How |
|------|-----|
| **What user captures** | `kubectl api-resources -o json > api-resources.json` |
| **What user passes** | `crane validate -i output/ --api-resources api-resources.json` |
| **New commands** | None |
| **New file formats** | None -- uses native Kubernetes `APIResourceList` JSON |
| **New flags** | `--api-resources` on `crane validate` |
| **Parsing** | `json.Unmarshal` into `APIResourceList` -- zero custom parsing |
| **Internal change** | Build discovery index from JSON; decouple index building from matching |
| **Scope of matching** | Identical to live mode -- strict GVK compatibility with suggestions |
| **Estimated effort** | ~2 days |
