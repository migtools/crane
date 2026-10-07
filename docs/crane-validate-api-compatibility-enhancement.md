**Suggested enhancement:**

**title:** crane-validate-api-compatibility

**authors:** TBD

**reviewers:** TBD

**approvers:** TBD

**creation-date:** 2026-04-21

**last-updated:** 2026-04-30

**status:** provisional

**see-also:**

- [https://github.com/migtools/crane/issues/185](https://github.com/migtools/crane/issues/185)  
- [https://github.com/migtools/crane/issues/319](https://github.com/migtools/crane/issues/319)
- [/enhancements/crane-2.0/export-group-mapping](https://github.com/konveyor/enhancements/tree/master/enhancements/crane-2.0/export-group-mapping)

**replaces:** N/A

**superseded-by:** N/A

# **Validate API Compatibility Before Apply**

## **Release Signoff Checklist**

- Enhancement is implementable  
- Design details are appropriately documented from clear requirements  
- Test plan is defined  
- User-facing documentation is created

## **Open Questions**

1. Should crane validate also check that CRDs backing custom resources are installed on the target, or only check built-in API discovery rows?
2. Should the command support a --fix or --rewrite mode that rewrites manifests to a compatible apiVersion when one exists, or should that remain a transform plugin concern?
3. What is the canonical list of cohabitating resource groups to deduplicate (e.g., extensions/apps, authorization.openshift.io/rbac.authorization.k8s.io)? Should this be hardcoded or configurable?

## **Summary**

Crane's pipeline currently has no target-aware preflight step. Resources are exported from the source cluster, transformed via plugins, and rendered into final manifests by `crane apply` -- but if the target cluster does not serve the same group/version for a resource, applying those manifests fails with an opaque API error. Users discover incompatibilities only at `kubectl apply` time, after they have already invested effort in the export, transform, and apply phases.

This enhancement adds a new **crane validate** command -- a read-only preflight check that scans the final rendered manifests from `crane apply`'s output directory, queries the **target** cluster's discovery API, and reports which apiVersion+kind combinations are not available on the target under strict group/version matching. When an incompatible resource's kind exists under a different apiVersion on the target, validate suggests the alternative. This brings Crane toward feature parity with MTC's GVK compatibility warnings, giving operators early visibility into API skew between clusters.

The enhancement is delivered in two phases:

- **Phase 1 (implemented):** Live-cluster validation -- `crane validate` queries the target cluster's discovery API directly via kubeconfig.
- **Phase 2 (planned):** Offline/disconnected validation -- `crane validate --api-resources api-resources.json` accepts the JSON output of `kubectl api-resources -o json` for air-gapped environments, pre-provisioning checks, and CI/CD pipelines without cluster access.

## **Motivation**

Migration between Kubernetes or OpenShift versions frequently involves API changes:

- **Removed APIs:** extensions/v1beta1 Deployments removed in Kubernetes 1.16+, policy/v1beta1 PodSecurityPolicies removed in 1.25+.  
- **Moved groups:** Resources migrating from authorization.openshift.io to rbac.authorization.k8s.io, or from extensions to apps/networking.k8s.io.  
- **CRD-backed types:** Custom resources whose CRDs may not be installed on the target cluster.

Today, Crane exports using the source cluster's preferred versions (via discoverPreferredResources() in cmd/export/discover.go). The export-group-mapping enhancement improved version selection at export time, but this does not cover all cases -- particularly when source and target clusters are different Kubernetes/OpenShift major versions.

MTC (mig-controller) provides GVK compatibility warnings by comparing source and destination discovery. Crane lacks an equivalent. Without it, users must either manually cross-reference API versions or discover failures during apply, which is too late for CI pipelines and disruptive for manual workflows.

Additionally, many real-world migrations involve **disconnected or air-gapped environments** where the migration workstation has no direct network access to the target cluster. Live-only validation cannot serve these scenarios. Offline validation using a captured API surface snapshot closes this gap.

### **Goals**

- **Early detection:** Surface all API incompatibilities between final rendered manifests and the target cluster *before* `kubectl apply`, in a single report.  
- **CI-friendly:** Provide deterministic exit codes so crane validate can gate a migration pipeline -- non-zero exit when incompatible GVKs are found.  
- **Read-only:** Validate never modifies manifests or cluster state. It is a pure reporting command.  
- **Low privilege:** Validate only requires basic cluster authentication -- it uses the Kubernetes discovery API (`/api`, `/apis`) which is accessible to all authenticated users via the default `system:discovery` ClusterRole. No special RBAC is needed.  
- **Pipeline composability:** Validate fits naturally into the export -> transform -> apply -> validate flow, reading from `crane apply`'s output directory without requiring changes to other commands.  
- **Suggestion engine:** When an incompatible resource's kind is available under a different apiVersion on the target, suggest the alternative to guide the user.
- **Offline/disconnected support (Phase 2):** Support validation against air-gapped clusters by accepting `kubectl api-resources -o json` output as input, using the same matching logic as live mode.

### **Non-Goals**

- **Automatic manifest rewriting:** Validate reports problems; it does not fix them. Rewriting apiVersions is the domain of transform plugins.  
- **Schema validation:** This is not an OpenAPI schema validator. It checks whether the API *exists* on the target, not whether the manifest fields are valid for that API version.  
- **Source cluster access:** Validate operates on filesystem YAML only; it does not require a connection to the source cluster.  
- **Version negotiation or upgrade path recommendation:** Validate suggests alternative apiVersions when available (e.g., "available as apps/v1"), but does not provide upgrade path guidance or migration recipes.
- **GitOps without a known target cluster:** When crane outputs manifests into a GitOps repo where the target cluster is not yet decided, GVK validation is not applicable -- crane's job stops at producing valid Kubernetes YAML.

## **Proposal**

### **User Stories**

#### **Story 1: Operator validates before applying to production target (live mode)**

An operator has exported resources from an OpenShift cluster, transformed them, and rendered final manifests with `crane apply`. Before running `kubectl apply` on the target, they run:

```
crane validate --input-dir ./output --context prod-cluster
```

The output reports:

```
APIVERSION                          KIND              NAMESPACE  RESOURCE     STATUS        REASON                                                                                  SUGGESTION
apps/v1                             Deployment        prod       deployments  OK
v1                                  Service           prod       services     OK
extensions/v1beta1                  Deployment        prod                    Incompatible  API version extensions/v1beta1 not available on target cluster (available as apps/v1)   available as apps/v1
authorization.openshift.io/v1       RoleBinding       prod                    Incompatible  API version authorization.openshift.io/v1 not available on target cluster

Summary: 4 scanned, 2 compatible, 2 incompatible
Result: FAILED -- 2 resource(s) incompatible with target cluster
Error: validation failed: one or more resources are incompatible with the target cluster
```

The operator knows to go back and run transform plugins that handle the version mapping, then re-run apply and validate.

#### **Story 2: CI pipeline gates on validation**

A CI/CD pipeline runs:

```bash
crane export --namespace myapp --export-dir ./export --context source-cluster
crane transform --export-dir ./export
crane apply --export-dir ./export --transform-dir ./transform --output-dir ./output
crane validate --input-dir ./output --context $TARGET_CONTEXT
if [ $? -ne 0 ]; then
  echo "API incompatibilities detected -- aborting migration"
  exit 1
fi
kubectl apply -f ./output/resources/ --context $TARGET_CONTEXT
```

The non-zero exit code from crane validate halts the pipeline before any cluster modification occurs.

#### **Story 3: Post-transform validation confirms fixes**

After running crane transform with plugins that remap deprecated APIs, the operator runs the full pipeline and validates the final output:

```
crane apply --export-dir ./export --transform-dir ./transform --output-dir ./output
crane validate --input-dir ./output --context target-cluster
```

All entries report OK; exit code 0. The operator proceeds to `kubectl apply` with confidence.

```
Summary: 8 scanned, 8 compatible, 0 incompatible
Result: PASSED -- all resources compatible with target cluster
```

#### **Story 4: CRD-backed custom resources**

An application uses a CRD-backed type widgets.example.com/v1. The CRD is not installed on the target cluster. crane validate reports:

```
APIVERSION                          KIND              NAMESPACE  RESOURCE  STATUS        REASON                                                          SUGGESTION
example.com/v1                      Widget            prod                 Incompatible  API version example.com/v1 not available on target cluster
```

The operator installs the CRD on the target before applying.

#### **Story 5: Air-gapped / disconnected environment (Phase 2 -- offline mode)**

The target cluster is on a restricted network. A team member with cluster access captures the API surface:

```bash
# Person A: has kubectl access to target cluster (air-gapped network)
kubectl api-resources -o json > api-resources.json
# Transfer file out via approved channel (USB, bastion, etc.)
```

The migration team validates without cluster access:

```bash
# Person B: has the exported manifests (migration workstation, no cluster access)
crane export ...
crane transform ...
crane apply ...
crane validate --input-dir ./output --api-resources api-resources.json
```

The matching logic, report format, and exit codes are identical to live mode.

#### **Story 6: Pre-provisioning check (Phase 2 -- offline mode)**

The target cluster doesn't exist yet, but the team knows it will be OCP 4.14. Someone captures the API surface from a reference cluster:

```bash
kubectl api-resources -o json --context reference-ocp-414 > ocp-414-api-resources.json
crane validate --input-dir ./output --api-resources ocp-414-api-resources.json
```

#### **Story 7: CI/CD pipeline without cluster access (Phase 2 -- offline mode)**

```yaml
# .github/workflows/validate.yaml
jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Validate manifests against target cluster
        run: crane validate -i output/ --api-resources .crane/api-resources.json --validate-dir results/
      - uses: actions/upload-artifact@v4
        with:
          name: validation-report
          path: results/
```

For multi-cluster targeting, run validate once per cluster:

```yaml
strategy:
  matrix:
    target: [prod-east, prod-west, staging]
steps:
  - run: crane validate -i output/ --api-resources clusters/${{ matrix.target }}/api-resources.json
```

### **Implementation Details/Notes/Constraints**

#### **Command Structure**

New command at cmd/validate/validate.go, following the existing Cobra pattern (self-contained package, options struct with Complete/Validate/Run, PreRun with viper binding).

**Flags (Phase 1 -- implemented):**

| Flag | Description |
| :---- | :---- |
| `--input-dir` / `-i` | Root directory of final rendered manifests to scan (default: `"output"`, matching crane apply's --output-dir default) |
| `--validate-dir` | Directory where validation report and failure artifacts are saved (default: `"validate"`) |
| `--kubeconfig` | Path to target cluster kubeconfig |
| `--context` | Kubeconfig context for target cluster |
| `--output` / `-o` | Report file format: `json` (default) or `yaml`. Controls the format of the persisted report file. Table output is always printed to the terminal. |

**Additional flag (Phase 2 -- planned):**

| Flag | Description |
| :---- | :---- |
| `--api-resources` | Path to `kubectl api-resources -o json` output file. Enables offline validation. Mutually exclusive with `--kubeconfig`/`--context`. |

The command also inherits all standard kubeconfig flags from genericclioptions.ConfigFlags (--cluster, --server, --token, etc.) and global crane flags (--debug, --flags-file).

The command enforces `cobra.NoArgs` -- positional arguments are rejected to prevent typos like `crane validate ./manifests` from silently validating the default directory.

**Mutual exclusion (Phase 2):**

```
if --api-resources is set AND (--kubeconfig or --context is explicitly set):
    error: "--api-resources and --kubeconfig/--context are mutually exclusive;
           use --api-resources for offline validation or kubeconfig flags for live validation"
```

When `--api-resources` is NOT set, existing behavior is unchanged -- `crane validate` uses the default kubeconfig as it does today. No breaking changes.

#### **Pipeline Position**

Validate operates on the **final rendered manifests** from `crane apply`'s output directory, not the raw export or intermediate transform output. This is deliberate:

```
export -> transform -> apply -> validate -> kubectl apply
  (source)   (plugins)  (render)  (check)    (deploy)
```

- **Export** produces raw resources from the source cluster
- **Transform** runs plugins that may modify apiVersions, add/remove fields
- **Apply** renders final YAML via `kubectl kustomize` into the output directory
- **Validate** checks those final manifests against the target cluster's API surface (live or offline)
- **kubectl apply** deploys to the target cluster

Validating after apply (rendering) ensures that transform plugins' changes are reflected in the check. Validating raw exports would produce false positives for resources that transforms will fix.

#### **Scanner**

Walk the input directory tree recursively using `filepath.WalkDir`. For each .yaml/.yml/.json file:

- Split multi-document YAML using `k8s.io/apimachinery/pkg/util/yaml.NewDocumentDecoder` (proper YAML-spec-aware document splitting)
- Decode each document individually -- one bad document logs a warning and continues to the next, without aborting remaining documents in the file
- Extract apiVersion, kind, metadata.namespace from each document
- Skip documents missing apiVersion or kind (e.g., empty documents, comments-only documents)
- Skip the `failures/` subdirectory to avoid re-scanning previous validation output
- Collect distinct (group, version, kind, namespace) tuples, tracking source files for each
- Deduplicate by GVK+namespace and sort results by group/version/kind/namespace

Input filenames are sanitized when writing failure artifacts to prevent path traversal from malicious manifest content.

#### **Target Discovery**

**Live mode (Phase 1):** Use client-go discovery client:

- Call `ServerGroupsAndResources()` on the target cluster
- Handle partial discovery failures gracefully (continue with available groups, log warning)
- Call `discoveryClient.Invalidate()` before querying to ensure fresh results (no stale cache)
- Build a two-level lookup map: groupVersion -> kind -> APIResource

**Offline mode (Phase 2):** Parse `kubectl api-resources -o json` output:

- Deserialize the native Kubernetes `APIResourceList` JSON with `json.Unmarshal`
- Build the same two-level lookup map as live mode
- Zero custom parsing -- the JSON output uses the same Go types (`metav1.APIResource`) the discovery client returns internally

The JSON output from `kubectl api-resources -o json` provides everything needed:

| JSON field | Maps to | Used for |
|------------|---------|----------|
| `group` + `version` | `groupVersion` (e.g., `apps/v1`; core resources omit `group`, so groupVersion = `v1`) | GVK matching |
| `kind` | `kind` | GVK matching |
| `name` | `resourcePlural` | Report output |
| `namespaced` | `namespaced` | Future: scope validation |

**Permissions (live mode):** Only requires the Kubernetes discovery API (`/api`, `/apis`), which is accessible to all authenticated users via the default `system:discovery` ClusterRoleBinding -> `system:authenticated` group. No namespace-level RBAC or resource-read permissions are needed.

**Permissions (offline mode):** No cluster access needed from the migration workstation. The person capturing the API resources file needs only basic authentication on the target cluster.

#### **Matching Logic**

For each (group, version, kind) from the scanned manifests:

1. Look up the groupVersion in the discovery index
2. If groupVersion not found: mark Incompatible with reason "API version X not available on target cluster"
3. If groupVersion found but kind not present: mark Incompatible with reason "kind X not found in API version Y on target cluster"
4. If both match: mark OK and populate the resource plural name
5. For incompatible resources: check if the same kind is available under a different apiVersion on the target (suggestion engine). If so, populate the Suggestion field (e.g., "available as apps/v1") and append it to the Reason for visibility in both table and JSON output.

The matching logic is identical for live and offline modes -- only the source of the discovery index differs.

#### **Output**

**Terminal (always):** Human-readable table with columns: APIVERSION, KIND, NAMESPACE, RESOURCE, STATUS, REASON, SUGGESTION. Followed by a summary line and a clear result line:

- `Result: PASSED -- all resources compatible with target cluster`
- `Result: FAILED -- N resource(s) incompatible with target cluster`

**Report file (persisted to --validate-dir):** Written as `report.json` (default) or `report.yaml` (with `-o yaml`). Contains the full ValidationReport struct including:

- `mode`: `"live"` or `"offline"` -- indicates how the target API surface was sourced
- `clusterContext`: kubeconfig context name (live mode)
- `apiResourcesSource`: file path (offline mode)
- `results`: array of ValidationResult entries
- `totalScanned`, `compatible`, `incompatible`: summary counts

**Failure artifacts:** When incompatibilities are found, individual YAML files are written to `--validate-dir/failures/`, one per incompatible GVK+namespace. Files are named `Kind_group_version_namespace.yaml` (matching export's naming pattern). All filename components are sanitized to prevent path traversal.

#### **Exit Codes**

| Code | Meaning |
| :---- | :---- |
| 0 | All GVKs compatible |
| 1 | One or more incompatible GVKs found, or another error occurred |

### **Security, Risks, and Mitigations**

**Target cluster access (live mode):** Validate requires read access to the target cluster's discovery API. This is a low-privilege operation -- discovery is available to all authenticated users via the default `system:discovery` ClusterRole. No special RBAC setup is needed. The kubeconfig must be handled securely.

**No write operations:** Validate is strictly read-only on the cluster. It only writes to the local filesystem (report and failure artifacts under --validate-dir).

**Path traversal prevention:** Failure artifact filenames are derived from manifest content (kind, apiVersion, namespace). All components are sanitized through `safeFilePart()` which strips path separators and non-alphanumeric characters, preventing directory escape.

**False positives with CRDs:** If a CRD is installed on the target but its API group hasn't been aggregated yet (e.g., during CRD installation), validate may report a false incompatibility. Documentation should note this timing consideration.

**Stale discovery cache (live mode):** The command calls `discoveryClient.Invalidate()` before querying to ensure fresh results from the target cluster's API server.

**Stale data (offline mode):** The `kubectl api-resources -o json` file is a point-in-time snapshot. Users should re-capture after cluster upgrades or CRD changes. The report includes the source file path and mode for traceability.

## **Design Details**

### **Architecture**

#### Phase 1: Live Mode (implemented)

```
crane validate -i output/ --context target-cluster
```

```
crane validate
    |
    +-- Scanner (filesystem)
    |   +-- Walk --input-dir (default: output/)
    |   +-- Split multi-doc YAML via NewDocumentDecoder
    |   +-- Decode each document individually (resilient to bad docs)
    |   +-- Collect deduplicated (group, version, kind, namespace) tuples
    |
    +-- Discovery (target cluster)
    |   +-- Connect via --kubeconfig / --context
    |   +-- Call ServerGroupsAndResources()
    |   +-- Build groupVersion -> kind -> APIResource lookup map
    |
    +-- Matcher
    |   +-- For each scanned GVK, check target lookup
    |   +-- Suggest alternatives when kind exists under different apiVersion
    |   +-- Classify: OK / Incompatible with reason and suggestion
    |
    +-- Reporter
        +-- Table to terminal (always)
        +-- Report file to --validate-dir (JSON or YAML)
        +-- Failure YAML artifacts to --validate-dir/failures/
        +-- Clear PASSED/FAILED result line + exit code
```

#### Phase 2: Live + Offline Mode (planned)

```
crane validate -i output/ --api-resources api-resources.json
```

```
crane validate
    |
    +-- Scanner (filesystem) -- unchanged
    |
    +-- Discovery index source (one of):
    |   +-- Live: configFlags.ToDiscoveryClient() -> buildDiscoveryIndex()
    |   +-- Offline: ParseAPIResourcesJSON(path) -> same index type
    |
    +-- Matcher -- unchanged (operates on pre-built index)
    |   +-- MatchResultsFromIndex(entries, index)
    |
    +-- Reporter -- unchanged (includes mode in report)
```

Key refactoring: decouple index building from matching. Currently `MatchResults` takes a `DiscoveryClient` and builds the index internally. Phase 2 requires:

1. Extract the discovery index as a public type (`DiscoveryIndex`)
2. Add `ParseAPIResourcesJSON(path)` that deserializes the kubectl JSON output and builds the same index type
3. Create `MatchResultsFromIndex(entries, index)` that takes a pre-built index
4. Keep `MatchResults` as a convenience wrapper (calls `buildDiscoveryIndex` then `MatchResultsFromIndex`)

Matching logic (`matchEntry`, `addSuggestion`, `buildKindIndex`) stays untouched.

### **Report Schema (JSON output)**

**Live mode:**

```json
{
  "mode": "live",
  "clusterContext": "target-cluster",
  "results": [
    {
      "apiVersion": "apps/v1",
      "kind": "Deployment",
      "namespace": "prod",
      "resourcePlural": "deployments",
      "status": "OK"
    },
    {
      "apiVersion": "extensions/v1beta1",
      "kind": "Deployment",
      "namespace": "prod",
      "status": "Incompatible",
      "reason": "API version extensions/v1beta1 not available on target cluster (available as apps/v1)",
      "suggestion": "available as apps/v1"
    }
  ],
  "totalScanned": 2,
  "compatible": 1,
  "incompatible": 1
}
```

**Offline mode (Phase 2):**

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

### **Output on Disk**

```
validate/
+-- report.json          # or report.yaml with -o yaml
+-- failures/
    +-- Deployment_extensions_v1beta1_prod.yaml
    +-- Route_route.openshift.io_v1_prod.yaml
```

### **Offline Mode: Parsing Logic (Phase 2)**

```go
// internal/validate/api_resources.go

// ParseAPIResourcesJSON reads the JSON output of `kubectl api-resources -o json`
// and builds the same discovery index used by the live-cluster code path.
func ParseAPIResourcesJSON(path string) (DiscoveryIndex, error) {
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

    index := DiscoveryIndex{}
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
type APIResourceListJSON struct {
    Kind       string                 `json:"kind"`
    APIVersion string                 `json:"apiVersion"`
    Resources  []APIResourceEntryJSON `json:"resources"`
}

type APIResourceEntryJSON struct {
    Name       string   `json:"name"`
    Namespaced bool     `json:"namespaced"`
    Group      string   `json:"group,omitempty"`
    Version    string   `json:"version"`
    Kind       string   `json:"kind"`
    Verbs      []string `json:"verbs,omitempty"`
}
```

No heuristics. No column detection. Just `json.Unmarshal`.

### **Internal Package Structure**

| File | Purpose |
| :---- | :---- |
| `cmd/validate/validate.go` | Cobra command: flags, Complete/Validate/Run, PreRun with viper |
| `cmd/validate/validate_test.go` | Flag registration, validation, and NoArgs tests |
| `internal/validate/types.go` | Data types: ManifestEntry, ValidationResult, ValidationReport, ErrValidationFailed |
| `internal/validate/scanner.go` | Recursive dir walker + NewDocumentDecoder-based multi-doc YAML parser + dedup |
| `internal/validate/scanner_test.go` | 10 tests: single-doc, multi-doc, nested dirs, dedup, core/named group parsing, cluster-scoped, skip failures dir, skip non-YAML |
| `internal/validate/matcher.go` | Discovery index builder, GVK matching, MatchResultsFromIndex, suggestion engine |
| `internal/validate/matcher_test.go` | 8 tests: all OK, missing GV, missing kind, mixed, suggestions, no suggestions, empty, resource plural |
| `internal/validate/report.go` | Table/JSON/YAML formatters, WriteFailures with filename sanitization |
| `internal/validate/report_test.go` | 4 tests: table formatting, JSON round-trip, empty report |
| `internal/validate/api_resources.go` | *(Phase 2)* ParseAPIResourcesJSON -- deserialize kubectl JSON into DiscoveryIndex |
| `internal/validate/api_resources_test.go` | *(Phase 2)* JSON parsing tests: valid, empty, malformed, core vs non-core groups |

### **Test Plan**

**Phase 1 (implemented):**

- **Unit tests (23 tests):**
  - YAML scanner: single-doc, multi-doc, nested directories, malformed YAML resilience, empty files, non-YAML files, failures dir skipping, deduplication, core group parsing, named group parsing, cluster-scoped resources
  - Matcher (with mock discovery client): exact match, missing group/version, missing kind, mixed results, suggestion when alternative exists, no suggestion when kind not on target, empty entries, resource plural populated
  - Reporter: table output formatting and column headers, JSON encoding and round-trip, empty report
  - Command: flag registration and defaults, flag validation (missing dir, not-a-directory, invalid output format, valid formats), positional argument rejection (cobra.NoArgs)
- **Integration tests:**
  - Mock discovery client returning controlled API group lists
  - Validate against a directory with known compatible and incompatible resources
  - Verify exit codes for all-compatible, some-incompatible, and error scenarios
- **E2E tests:**
  - Full pipeline: export from source -> transform -> apply -> validate against target with known API differences -> confirm report accuracy
  - Confirm that resources reported as Incompatible actually fail on kubectl apply, and those reported OK succeed
- **Regression:** Existing export/transform/apply tests unaffected (validate is additive)

**Phase 2 (planned):**

- **Unit tests:** JSON parsing -- valid output, empty resources, malformed JSON, core resources (no group), non-core resources (with group), duplicate kinds across groups
- **Unit tests:** Offline matching -- same scenarios as live matcher tests but with a parsed index from JSON instead of a discovery client
- **Unit tests:** Mutual exclusion -- `--api-resources` with `--context` or `--kubeconfig` returns error
- **Integration tests:** Capture `kubectl api-resources -o json` from a test cluster, run `crane validate --api-resources` with it -- ensure results match live validate against the same cluster
- **E2E tests:** Cross-platform scenarios (EKS api-resources validated against OCP manifests)
- **Regression:** Live-mode validate behavior unchanged

### **Offline Mode: Edge Cases (Phase 2)**

| Concern | Handling |
|---------|----------|
| **Stale data** | Document that the file is a point-in-time snapshot. Users should re-capture after cluster upgrades or CRD changes. |
| **CRDs installed after capture** | Same as above -- re-run the kubectl command. |
| **Empty resources array** | Error: "api-resources file contains no resources" |
| **Malformed JSON** | Error from `json.Unmarshal` with clear message. |
| **Missing `kind` field at top level** | Warn if `kind` is not `APIResourceList`, but still attempt to parse. |
| **Core resources (no `group` field)** | `group` is omitted for core API resources. Parser uses `version` alone as `groupVersion` (e.g., `"v1"`). |
| **Duplicate resources across groups** | Multiple entries with the same kind but different group/version are valid (e.g., `Event` in `v1` and `events.k8s.io/v1`). Index handles this naturally since it's keyed by `groupVersion`. |
| **Verbs / RBAC** | Out of scope. `verbs` field is present in JSON but ignored. |
| **Multi-cluster** | `--api-resources` takes one file. For N clusters, run validate N times with the corresponding file. |
| **kubectl version compatibility** | `kubectl api-resources -o json` is available since K8s 1.11+. The `APIResourceList` schema is stable. |
| **GitOps without target cluster** | When crane outputs to a GitOps repo and no target cluster is known yet, offline validate is not applicable. GVK validation requires a target cluster's API surface. |

### **Implementation Plan (Phase 2)**

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

### **Upgrade / Downgrade Strategy**

- **Additive command:** crane validate is a new command. No existing behavior changes. Users who don't invoke it see no difference.
- **No data format changes:** Validate reads the existing apply output directory format. It does not modify it.
- **Offline mode (Phase 2):** Adding `--api-resources` is backwards-compatible. When the flag is not set, behavior is identical to Phase 1.

## **Implementation History**

- **#185:** Exploration of MTC's resource versioning logic -- design input for this enhancement. *(Open)*
- **#230:** Feature story for this enhancement. *(Open)*
- **#302:** Phase 1 implementation PR -- crane validate command with live-cluster validation, 23 unit tests. *(Merged)*
- **#319:** Phase 2 feature request -- offline/disconnected cluster support. *(Open)*

## **Drawbacks**

- **Additional step in the pipeline:** Users must remember to run crane validate -- it is not automatically invoked by crane apply. This is deliberate (composability), but means users can still skip it.
- **Strict matching may over-report:** Some resources may work on the target under a different version of the same group (e.g., v1beta1 -> v1), but strict matching flags them as incompatible. The suggestion engine mitigates this by showing available alternatives.
- **Discovery limitations (live mode):** The Kubernetes discovery API may not surface all available API versions in all configurations (e.g., aggregated API servers with intermittent availability). The command handles partial discovery failures gracefully.
- **Stale data (offline mode):** The api-resources JSON file is a point-in-time snapshot that may drift from the actual cluster state. Documentation should emphasize re-capturing after changes.

## **Alternatives**

1. **Integrate validation into crane apply as a preflight step:** Rejected because it couples a read-only check to a write command, violating Crane's Unix-philosophy composability. Users may want to validate without being ready to apply, or validate in a different environment than where apply runs.
2. **Validate at export time against both source and target:** Rejected because export is source-only by design, and requiring target access during export adds a dependency that doesn't exist today. Validate as a separate command keeps concerns cleanly separated.
3. **Transform plugin that rewrites apiVersions:** Complementary, not a replacement. A transform plugin can fix known version mappings, but the user still needs a way to discover *which* mappings are needed. Validate provides that discovery.
4. **Static compatibility database:** Ship a hardcoded map of "version X removed in Kubernetes Y." Rejected because it cannot cover CRDs, custom API servers, or OpenShift-specific APIs, and requires constant maintenance.
5. **Custom offline format (Phase 2 alternative):** Define a crane-specific YAML format for target API surface. Rejected in favor of `kubectl api-resources -o json` because it requires zero custom parsing, uses native Kubernetes types, and needs no crane tooling on the target cluster side.

---

**Revised GitHub Issue Body for [#230](https://github.com/migtools/crane/issues/230):**

# **Add validation to detect API incompatibilities before applying exported resources to target cluster**

## **Summary**

Add a new **crane validate** command -- a read-only preflight check that scans the final rendered manifests from `crane apply`'s output directory against the **target** cluster's discovery API and reports apiVersion+kind combinations that are not available on the target. This gives operators early visibility into API incompatibilities (removed APIs, moved groups, missing CRDs) before `kubectl apply`, matching MTC's GVK compatibility warnings.

Pipeline position: export -> transform -> apply -> **validate** -> kubectl apply

Supports two modes:
- **Live mode:** Queries target cluster discovery directly via kubeconfig (Phase 1, implemented)
- **Offline mode:** Accepts `kubectl api-resources -o json` output for air-gapped environments (Phase 2, planned)

Full design and specification: [konveyor/enhancements/crane-2.0/validate-api-compatibility](https://github.com/konveyor/enhancements/tree/master/enhancements/crane-2.0/validate-api-compatibility) *(TODO: link once enhancement PR is merged)*

Follow-up from #185 (MTC resource versioning exploration).

## **Acceptance Criteria**

### Phase 1 (implemented in #302)

- New crane validate command accepts `--input-dir` (defaulting to `output/`, crane apply's output) plus target cluster `--kubeconfig`/`--context`.
- Scans all YAML/JSON in the directory tree using proper YAML document splitting, extracts distinct apiVersion+kind+namespace tuples, and checks each against the target's discovery API using strict group/version + kind matching.
- Always prints a human-readable table to the terminal showing each GVK as OK or Incompatible with reason and suggestion. Includes a clear PASSED/FAILED result line.
- Persists a report file to `--validate-dir` in JSON (default) or YAML (`-o yaml`) for downstream tooling.
- Writes per-resource failure artifacts as YAML files to `--validate-dir/failures/` for auditability.
- Suggests alternative apiVersions when the same kind exists under a different group/version on the target.
- Exit codes are deterministic: 0 = all compatible, 1 = incompatible GVKs found or error.
- Read-only: no modifications to manifests or target cluster state. Only requires discovery API access (available to all authenticated users).
- 23 unit tests covering YAML scanning, GVK matching, suggestion engine, report formatting, flag validation, and argument rejection.

### Phase 2 (planned in #319)

- New `--api-resources` flag accepts path to `kubectl api-resources -o json` output for offline validation.
- Mutually exclusive with `--kubeconfig`/`--context`.
- Uses identical matching logic, report format, and exit codes as live mode.
- Report includes mode (`live`/`offline`) and source information for traceability.
- Unit tests for JSON parsing, offline matching, and mutual exclusion.
