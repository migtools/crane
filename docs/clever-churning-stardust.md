# Implementation Plan: `crane validate` Command

## Context

Crane's `export` command is source-only -- it cannot compare the target cluster. MTC/mig-controller provides operators early visibility when source GVKs are not available on the destination. Per [GitHub issue #230](https://github.com/migtools/crane/issues/230) and the Option A design in `docs/mtc-resource-versioning-and-crane-options.md`, we add a **dedicated read-only command** (`crane validate`) that answers: *given this bundle of manifests, does the target cluster expose the same strict discovery rows?*

This is Phase 1 only. Source-live mode and CRD apiextensions skew detection are Phase 2.

---

## File Structure

```
cmd/validate/
    validate.go              # Cobra command, Options, Complete/Validate/Run
    validate_test.go          # Flag parsing, validation, command registration tests

internal/validate/
    types.go                 # Shared data types
    scanner.go               # Filesystem walk, YAML parsing, GVK extraction
    scanner_test.go
    matcher.go               # Target discovery fetch, strict GVK matching, cohabitating dedupe
    matcher_test.go
    report.go                # Table + JSON formatters
    report_test.go

main.go                      # Add one import + one AddCommand line
```

---

## Step 1: Data Types (`internal/validate/types.go`)

```go
// ManifestEntry is one distinct apiVersion+kind+namespace tuple from scanned files.
type ManifestEntry struct {
    APIVersion  string
    Kind        string
    Group       string   // parsed from APIVersion (e.g. "apps" from "apps/v1")
    Version     string   // parsed from APIVersion (e.g. "v1")
    Namespace   string   // from metadata.namespace; empty for cluster-scoped
    SourceFiles []string // which files contributed this entry
}

type ValidationStatus string
const (
    StatusOK           ValidationStatus = "OK"
    StatusIncompatible ValidationStatus = "Incompatible"
)

// ValidationResult is one row in the final report.
type ValidationResult struct {
    APIVersion     string           `json:"apiVersion"`
    Kind           string           `json:"kind"`
    Namespace      string           `json:"namespace,omitempty"`
    ResourcePlural string           `json:"resourcePlural,omitempty"`
    Status         ValidationStatus `json:"status"`
    Reason         string           `json:"reason,omitempty"`
}

// ValidationReport is the complete output.
type ValidationReport struct {
    Results      []ValidationResult `json:"results"`
    TotalScanned int                `json:"totalScanned"`
    Compatible   int                `json:"compatible"`
    Incompatible int                `json:"incompatible"`
}

func (r *ValidationReport) HasIncompatible() bool { return r.Incompatible > 0 }

// Sentinel error for CI exit code.
var ErrIncompatibleResources = fmt.Errorf("one or more resources are incompatible with the target cluster")
```

---

## Step 2: Scanner (`internal/validate/scanner.go`)

**Purpose:** Walk directories recursively, parse YAML (including multi-doc), extract distinct `(apiVersion, kind, namespace)` tuples.

**Why not reuse `internal/file.ReadFiles`:** It does not handle multi-document YAML (one doc per file only). The scanner needs its own multi-doc handling.

**Implementation:**
1. `filepath.WalkDir` each input directory
2. Skip `failures/` directories and non-`.yaml`/`.yml`/`.json` files
3. For each file, use `k8s.io/apimachinery/pkg/util/yaml.NewYAMLOrJSONDecoder` to iterate documents (already a transitive dependency -- handles `---` separators, comments, empty docs)
4. For each document: unmarshal minimal fields (`apiVersion`, `kind`, `metadata.namespace`), parse group/version via `schema.ParseGroupVersion()` (same as `cmd/export/discover.go:224`)
5. Deduplicate using `map[string]*ManifestEntry` keyed by `"{group}/{version}/{kind}/{namespace}"`
6. Return sorted slice

```go
type ScanOptions struct {
    Dirs []string
}

func ScanManifests(opts ScanOptions, log logrus.FieldLogger) ([]ManifestEntry, error)
```

**Test cases (`scanner_test.go`):**
- Single-doc YAML, multi-doc YAML, nested dirs
- `failures/` dir skipped
- Non-YAML files ignored
- Deduplication across files
- Core group parsing (`v1` -> group="", version="v1")
- Named group parsing (`apps/v1` -> group="apps", version="v1")

---

## Step 3: Matcher (`internal/validate/matcher.go`)

**Purpose:** Fetch target discovery, perform strict GVK matching, handle cohabitating resources.

**Cohabitating resources** (fixed list from MTC docs, `docs/mtc-resource-versioning-and-crane-options.md` section 2):

```go
var cohabitatingResources = map[string][]string{
    "deployments":     {"extensions", "apps"},
    "daemonsets":      {"extensions", "apps"},
    "replicasets":     {"extensions", "apps"},
    "networkpolicies": {"extensions", "networking.k8s.io"},
    "events":          {"", "events.k8s.io"},
}
```

**Implementation:**
1. Call `discoveryClient.ServerPreferredResources()` -- handle partial errors (log warning, continue). Unlike export's `discoverPreferredResources`, do NOT filter by verbs (validate only checks existence, not CRUD ability).
2. Additionally call `discoveryClient.ServerResources()` (or `ServerGroupsAndResources()`) to get ALL served versions, not just preferred. This is critical: if an export produced `extensions/v1beta1` Deployments, we need to check whether that specific version is served on the target, even if the target's preferred version is `apps/v1`. Strict matching means checking the exact group/version from the manifest.
3. Build lookup index: `map[groupVersionString]map[kind]metav1.APIResource`
4. For each `ManifestEntry`:
   - Look up `{group}/{version}` in index
   - If group/version not found: `Incompatible`, reason: `"API version {apiVersion} not available on target cluster"`
   - If found but kind missing: `Incompatible`, reason: `"kind {kind} not found in API version {apiVersion} on target cluster"`
   - If found: `OK`, record resource plural
5. Cohabitating dedupe: After matching, if two entries for the same kind come from groups in the same cohabitating set and one is OK, suppress the incompatible one (add a note). If both are incompatible, report only one.

```go
type MatchOptions struct {
    DiscoveryClient discovery.DiscoveryInterface
}

func MatchResults(entries []ManifestEntry, opts MatchOptions, log logrus.FieldLogger) (*ValidationReport, error)
```

**Test cases (`matcher_test.go`):** Use `k8s.io/client-go/discovery/fake.FakeDiscovery` with canned `ServerPreferredResources` responses.
- All GVKs found -> all OK
- Missing GroupVersion -> Incompatible
- GroupVersion present, kind missing -> Incompatible
- Mixed results
- Cohabitating dedupe (extensions Deployment incompatible, apps/v1 Deployment OK -> suppress extensions warning)
- Partial discovery failure -> log warning, continue
- Empty entry list -> zero-result report

---

## Step 4: Report Formatter (`internal/validate/report.go`)

**Table format** using `github.com/olekukonko/tablewriter` (already a direct dependency, used in `cmd/plugin-manager/list/list.go:12`):

```
APIVERSION             KIND        NAMESPACE   RESOURCE      STATUS        REASON
apps/v1                Deployment  prod        deployments   OK
v1                     ConfigMap   prod        configmaps    OK
route.openshift.io/v1  Route       prod                      Incompatible  API version route.openshift.io/v1 not available on target cluster

Summary: 3 scanned, 2 compatible, 1 incompatible
```

**JSON format** via `encoding/json.MarshalIndent` -- outputs the full `ValidationReport` struct.

```go
func FormatTable(w io.Writer, report *ValidationReport)
func FormatJSON(w io.Writer, report *ValidationReport) error
```

**Test cases (`report_test.go`):** Verify table contains expected columns/rows; JSON round-trips to `ValidationReport`.

---

## Step 5: Command Wiring (`cmd/validate/validate.go`)

Follows the exact pattern from `cmd/export/export.go` (Complete->Validate->Run, viper PreRun, ConfigFlags).

```go
type ValidateOptions struct {
    configFlags      *genericclioptions.ConfigFlags
    cobraGlobalFlags *flags.GlobalFlags
    globalFlags      *flags.GlobalFlags
    exportDir        string
    outputDir        string // optional: additionally scan crane apply output dir
    outputFormat     string // "table" (default) or "json"
    genericclioptions.IOStreams
}
```

**Flags:**

| Flag | Short | Default | Purpose |
|------|-------|---------|---------|
| `--export-dir` | `-e` | `"export"` | Primary scan directory (same name as export/apply) |
| `--output-dir` | | `""` | Optional: also scan apply output dir |
| `--output` | `-o` | `"table"` | Report format: `table` or `json` |
| `--kubeconfig` | | | Target cluster kubeconfig (via ConfigFlags) |
| `--context` | | | Target cluster context (via ConfigFlags) |

Note: `-o` is safe here because validate doesn't use the ConfigFlags `-o` (that's for kubectl output, not used by crane commands).

**Complete():**
- Load raw kubeconfig via `o.configFlags.ToRawKubeConfigLoader().RawConfig()` -- validates cluster is reachable

**Validate():**
- Check `--export-dir` path exists and is a directory
- If `--output-dir` set, check it exists
- Check `--output` is `"table"` or `"json"`

**Run():**
1. Build scan dirs list (exportDir, optionally outputDir)
2. `validate.ScanManifests(...)` -- scan YAML files
3. `o.configFlags.ToDiscoveryClient()` -> `Invalidate()` for fresh data
4. `validate.MatchResults(entries, MatchOptions{DiscoveryClient: discoveryClient}, log)`
5. Format to `o.IOStreams.Out` based on `--output`
6. If `report.HasIncompatible()`, return `ErrIncompatibleResources` (causes exit code 1)

**Exit codes:**
- `0`: All GVKs compatible
- `1`: Incompatible resources found OR any other error (cobra default)

**Registration (`main.go`):**
```go
import "github.com/konveyor/crane/cmd/validate"
// ...
root.AddCommand(validate.NewValidateCommand(
    genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}, f))
```

**Test cases (`validate_test.go`):** Follow `cmd/export/export_test.go` patterns.
- `TestNewValidateCommand`: verifies flag names, defaults
- `TestValidate`: table-driven for flag combinations (missing export-dir, invalid output format)

---

## Step 6: Registration in `main.go`

Add one import and one `AddCommand` line, following existing pattern.

**File:** `main.go:10` (imports) and `main.go:28` area (AddCommand calls)

---

## Implementation Order

| Phase | Files | Validates with |
|-------|-------|----------------|
| 1 | `internal/validate/types.go` | `go build ./internal/validate/` |
| 2 | `internal/validate/scanner.go` + `scanner_test.go` | `go test ./internal/validate/` |
| 3 | `internal/validate/matcher.go` + `matcher_test.go` | `go test ./internal/validate/` |
| 4 | `internal/validate/report.go` + `report_test.go` | `go test ./internal/validate/` |
| 5 | `cmd/validate/validate.go` + `validate_test.go` | `go test ./cmd/validate/` |
| 6 | `main.go` (add import + AddCommand) | `go build -o crane .` |

---

## Verification

1. **Unit tests:** `go test ./internal/validate/... ./cmd/validate/...`
2. **Full test suite:** `go test $(go list ./... | grep -v '/e2e-tests/')`
3. **Build:** `go build -o crane .`
4. **Manual test with real export:**
   ```bash
   # Export from source cluster
   ./crane export -n my-ns --kubeconfig source.kubeconfig -e /tmp/test-export
   # Validate against target cluster
   ./crane validate -e /tmp/test-export --kubeconfig target.kubeconfig --context target-ctx
   ./crane validate -e /tmp/test-export --kubeconfig target.kubeconfig -o json
   # Check exit code
   echo $?
   ```
5. **CI scenario:** `./crane validate -e export-dir --kubeconfig target.kubeconfig -o json && ./crane apply ...`

---

## Key Files to Reference During Implementation

| File | Why |
|------|-----|
| `cmd/export/export.go` | Canonical Complete/Validate/Run + flag pattern |
| `cmd/export/discover.go` | Discovery client usage, `schema.ParseGroupVersion`, `groupResource` struct |
| `cmd/export/export_test.go` | Test patterns (table-driven, ConfigFlags setup) |
| `cmd/apply/apply.go` | Flags struct with `mapstructure` tags, viper PreRun pattern |
| `internal/file/file_helper.go` | Existing file reading (reference for dir walk, skip logic) |
| `cmd/plugin-manager/list/list.go` | `tablewriter` usage pattern |
| `main.go` | Command registration pattern |
| `docs/mtc-resource-versioning-and-crane-options.md` | MTC strict matching rules, cohabitating resources list |
