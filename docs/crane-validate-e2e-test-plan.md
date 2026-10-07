# Test Plan: `crane validate` — API Compatibility Validation

**Enhancement:** [#230 — Validate API Compatibility Before Apply](https://github.com/migtools/crane/issues/230)
**Date:** 2026-04-30
**Status:** Draft

---

## 1. Scope

This test plan covers the `crane validate` command, which performs read-only preflight checks against a target cluster's discovery API (live mode) or a captured `kubectl api-resources -o json` file (offline mode) to detect GVK (Group/Version/Kind) incompatibilities in final rendered YAML manifests before `kubectl apply`.

Pipeline position: `export → transform → apply → validate → kubectl apply`

The plan is organized into three tiers — unit, integration, and end-to-end — covering both live (online) and offline validation modes.

### Tier Definitions

| Tier | Meaning | Run When |
|------|---------|----------|
| **tier0** | Must-pass gate — core functionality, blocks merge | Every PR, every CI run |
| **tier1** | Standard coverage — important paths and common scenarios | Every PR, every CI run |
| **tier2** | Deep coverage — edge cases, stress tests, rare scenarios | Nightly / release gate |

---

## 2. Components Under Test

| Component | Location | Responsibility |
|-----------|----------|----------------|
| Scanner | `internal/validate/scanner.go` | Walk directories, parse multi-doc YAML via `NewDocumentDecoder`, extract GVK tuples, deduplicate |
| Matcher | `internal/validate/matcher.go` | Compare scanned GVKs against target discovery index, suggest alternatives |
| API Resources Parser | `internal/validate/api_resources.go` | Parse `kubectl api-resources -o json` into discovery index (offline mode) |
| Reporter | `internal/validate/report.go` | Format output (table/JSON/YAML), write failure artifacts with filename sanitization |
| Types | `internal/validate/types.go` | Data structures, `ErrValidationFailed`, `ValidationReport` with mode/source fields |
| Command | `cmd/validate/validate.go` | Cobra wiring, flags, `--api-resources` mutual exclusion, Complete/Validate/Run flow |
| E2E Pipeline | `e2e-tests/` | Full export → transform → apply → validate flow on real clusters |

---

## 3. Unit Tests

### 3.1 Scanner (`internal/validate/scanner_test.go`)

Tests for `ScanManifests()` which walks directories, splits multi-doc YAML via `NewDocumentDecoder`, and extracts deduplicated `ManifestEntry` tuples.

| ID | Tier | Test Case | Status | Expected |
|----|------|-----------|--------|----------|
| S-01 | tier0 | Single-document YAML | **Exists** | Returns 1 entry with `Group=apps`, `Version=v1`, `Kind=Deployment`, `APIVersion=apps/v1` |
| S-02 | tier0 | Multi-document YAML | **Exists** | Returns 2 entries from `---`-separated file |
| S-03 | tier0 | Core API group (apiVersion: v1) | **Exists** | `Group=""`, `Version="v1"` |
| S-04 | tier1 | Named API group (apps/v1) | **Exists** | `Group="apps"`, `Version="v1"` |
| S-05 | tier1 | Recursive directory walk | **Exists** | Both root and subdirectory files collected |
| S-06 | tier1 | Non-YAML files ignored | **Exists** | `.txt`, `.csv` skipped; only `.yaml` parsed |
| S-07 | tier1 | Deduplication by GVK+namespace | **Exists** | Two files with same GVK+ns → 1 entry with 2 SourceFiles |
| S-08 | tier1 | failures/ directory skipped | **Exists** | YAML files in `failures/` not scanned |
| S-09 | tier1 | Cluster-scoped resources | **Exists** | `Namespace=""` for cluster-scoped resources |
| S-10 | tier1 | CRD-backed resource | To add | `apiVersion: example.com/v1, kind: Widget` → `Group="example.com"` |
| S-11 | tier1 | Malformed YAML document in multi-doc | To add | Bad doc logged as warning, remaining docs parsed |
| S-12 | tier2 | Empty YAML file | To add | No entries returned, no error |
| S-13 | tier2 | YAML with no apiVersion/kind | To add | Entry skipped silently, no error |
| S-14 | tier2 | Empty directory | To add | Returns empty slice, no error |
| S-15 | tier1 | Nonexistent directory | To add | Returns error |
| S-16 | tier2 | Multi-document with empty documents | To add | Skips empty docs, returns valid entries |
| S-17 | tier1 | .json manifest file | To add | JSON-format manifest parsed correctly |
| S-18 | tier2 | .yml extension | To add | Treated same as .yaml |
| S-19 | tier2 | Invalid apiVersion format | To add | `apiVersion: apps/` → skipped or treated as invalid |
| S-20 | tier2 | CRLF line endings | To add | Windows-style `\r\n` parsed correctly |

### 3.2 Matcher (`internal/validate/matcher_test.go`)

Tests for `MatchResults()` (live path) and `MatchResultsFromIndex()` (offline path) which compare scanned entries against target discovery.

| ID | Tier | Test Case | Status | Expected |
|----|------|-----------|--------|----------|
| M-01 | tier0 | All entries compatible | **Exists** | All `StatusOK`, `Compatible=N`, `Incompatible=0` |
| M-02 | tier0 | Missing group/version | **Exists** | `StatusIncompatible`, reason: "API version X not available" |
| M-03 | tier0 | GV present but kind missing | **Exists** | `StatusIncompatible`, reason: "kind X not found in API version Y" |
| M-04 | tier0 | Mixed compatible and incompatible | **Exists** | Correct counts for compatible and incompatible |
| M-05 | tier1 | Suggestion when alternative GV exists | **Exists** | Suggestion field populated with alternative GV |
| M-06 | tier1 | No suggestion when kind not on target | **Exists** | Suggestion field empty |
| M-07 | tier1 | Empty entries | **Exists** | Empty report, no error |
| M-08 | tier1 | Resource plural populated | **Exists** | `ResourcePlural` filled from discovery |
| M-09 | tier0 | MatchResultsFromIndex — all OK | **Exists** | Same as M-01 but via pre-built index |
| M-10 | tier0 | MatchResultsFromIndex — incompatible | **Exists** | Same as M-02 but via pre-built index |
| M-11 | tier1 | MatchResultsFromIndex — suggestion | **Exists** | Same as M-05 but via pre-built index |
| M-12 | tier1 | Partial discovery failure (some groups fail) | To add | Compatible groups reported OK; failed groups handled gracefully |
| M-13 | tier2 | Multiple suggestions (kind in multiple GVs) | To add | All alternative GVs listed, sorted, comma-separated |
| M-14 | tier2 | Subresource filtering (names with `/`) | To add | `deployments/scale` filtered out of discovery index |
| M-15 | tier2 | Namespace preserved in results | To add | Result includes original namespace from manifest |
| M-16 | tier1 | Core group (v1) exact match | To add | `v1 Service` matches target's `v1` discovery |
| M-17 | tier2 | All entries incompatible | To add | `Compatible=0`, all `StatusIncompatible` |

### 3.3 API Resources Parser (`internal/validate/api_resources_test.go`)

Tests for `ParseAPIResourcesJSON()` which parses `kubectl api-resources -o json` for offline mode.

| ID | Tier | Test Case | Status | Expected |
|----|------|-----------|--------|----------|
| A-01 | tier0 | Valid JSON with mixed resources | **Exists** | Index built with correct GV→kind mappings |
| A-02 | tier0 | Core resources (group omitted) | **Exists** | `group=""` → groupVersion is `"v1"` |
| A-03 | tier0 | Non-core resources (group present) | **Exists** | `group="apps"` → groupVersion is `"apps/v1"` |
| A-04 | tier1 | Resource plural preserved | **Exists** | `name` field stored as resource plural |
| A-05 | tier0 | Empty resources array | **Exists** | Error: "contains no resources" |
| A-06 | tier0 | Malformed JSON | **Exists** | Error from json.Unmarshal |
| A-07 | tier1 | File not found | **Exists** | Error about reading file |
| A-08 | tier1 | Duplicate kind across groups | **Exists** | Both indexed under separate groupVersion keys |
| A-09 | tier2 | Namespaced flag preserved | **Exists** | `Namespaced` field stored correctly |

### 3.4 Reporter (`internal/validate/report_test.go`)

| ID | Tier | Test Case | Status | Expected |
|----|------|-----------|--------|----------|
| R-01 | tier0 | Table output — mixed results | **Exists** | All 7 column headers present, correct status values, summary line |
| R-02 | tier1 | Table output — empty report | **Exists** | Summary shows "0 scanned, 0 compatible, 0 incompatible" |
| R-03 | tier0 | JSON output and round-trip | **Exists** | Valid JSON, round-trips correctly |
| R-04 | tier1 | YAML output | To add | Valid YAML, round-trips correctly |
| R-05 | tier1 | Table PASSED/FAILED result line | To add | "PASSED" when 0 incompatible, "FAILED — N resource(s)" otherwise |
| R-06 | tier2 | Table mode header (live) | To add | "Mode: live (context: tgt)" |
| R-07 | tier2 | Table mode header (offline) | To add | "Mode: offline (api-resources: /path)" |
| R-08 | tier1 | WriteFailures — YAML-encoded files | To add | Failure files are valid YAML matching `.yaml` extension |
| R-09 | tier2 | WriteFailures — filename sanitization | To add | `safeFilePart()` replaces `/` and special chars, empty → "unknown" |
| R-10 | tier2 | WriteFailures — cluster-scoped uses "clusterscoped" | To add | Empty namespace → `_clusterscoped.yaml` in filename |
| R-11 | tier2 | WriteFailures — directory cleaned on re-run | To add | `os.RemoveAll` + `os.MkdirAll` clears old files |
| R-12 | tier2 | WriteFailures — no failures dir when all compatible | To add | No `failures/` directory created |

### 3.5 Command (`cmd/validate/validate_test.go`)

| ID | Tier | Test Case | Status | Expected |
|----|------|-----------|--------|----------|
| CMD-01 | tier0 | Command registration and flag names | **Exists** | `input-dir`, `validate-dir`, `output`, `api-resources` flags registered |
| CMD-02 | tier0 | Default flag values | **Exists** | `input-dir="output"`, `validate-dir="validate"`, `output="json"`, `api-resources=""` |
| CMD-03 | tier0 | Missing input-dir | **Exists** | Error containing "input-dir" |
| CMD-04 | tier1 | Input-dir is a file | **Exists** | Error "not a directory" |
| CMD-05 | tier0 | Invalid output format | **Exists** | Error about yaml/json |
| CMD-06 | tier1 | Valid yaml format | **Exists** | No error |
| CMD-07 | tier1 | Valid json format | **Exists** | No error |
| CMD-08 | tier0 | api-resources file not found | **Exists** | Error "api-resources file" |
| CMD-09 | tier0 | api-resources + context mutually exclusive | **Exists** | Error "mutually exclusive" |
| CMD-10 | tier0 | api-resources + kubeconfig mutually exclusive | **Exists** | Error "mutually exclusive" |
| CMD-11 | tier1 | api-resources valid file accepted | **Exists** | No error |
| CMD-12 | tier0 | NoArgs enforced | **Exists** | Positional args rejected |

---

## 4. Integration Tests

Integration tests use mock discovery clients or offline JSON files to simulate target cluster responses without requiring a real cluster. **Status: None exist yet — all to add.**

### 4.1 End-to-End Command Flow (mock cluster / offline)

| ID | Tier | Test Case | Setup | Expected |
|----|------|-----------|-------|----------|
| INT-01 | tier0 | All compatible (offline) | Temp dir with compatible manifests + matching api-resources.json | Exit 0, report shows all OK, mode=offline |
| INT-02 | tier0 | Some incompatible (offline) | Manifests with deprecated GVKs + api-resources.json missing them | Exit 1, correct incompatible count, failure files written |
| INT-03 | tier1 | All incompatible (offline) | All manifests reference absent GVKs | Exit 1, all entries Incompatible |
| INT-04 | tier1 | Empty input dir (offline) | No YAML files, valid api-resources.json | Exit 0, empty report |
| INT-05 | tier1 | Malformed api-resources file | Invalid JSON | Exit 1, "parsing api-resources JSON" error |
| INT-06 | tier1 | Empty api-resources file | Valid JSON, resources: [] | Exit 1, "contains no resources" |
| INT-07 | tier1 | Report written to validate-dir | Run with `--validate-dir ./out` | `out/report.json` created with correct content |
| INT-08 | tier1 | Failures directory populated | Run with incompatible resources | `validate-dir/failures/` contains one YAML file per incompatible GVK |
| INT-09 | tier1 | JSON report mode | `--output json` | Report file is valid JSON matching schema |
| INT-10 | tier2 | YAML report mode | `--output yaml` | Report file `report.yaml` is valid YAML |
| INT-11 | tier2 | Table always printed to stdout | Any run | Table printed regardless of `-o` flag |
| INT-12 | tier2 | Mode and source in report | Live: mode=live; Offline: mode=offline, apiResourcesSource set | Correct metadata in report |

### 4.2 Real-World API Migration Scenarios (offline mode with crafted api-resources)

| ID | Tier | Scenario | Source Manifests | Target API Surface | Expected |
|----|------|----------|------------------|--------------------|----------|
| MIG-01 | tier1 | OpenShift → K8s | `extensions/v1beta1 Deployment`, `route.openshift.io/v1 Route`, `v1 Service` | `apps/v1`, `v1` (no Route) | Deployment: Incompatible (suggestion: apps/v1); Route: Incompatible (no suggestion); Service: OK |
| MIG-02 | tier1 | K8s 1.15 → 1.25+ (PSP removal) | `policy/v1beta1 PodSecurityPolicy`, `apps/v1 Deployment` | `apps/v1` (no policy/v1beta1) | PSP Incompatible, Deployment OK |
| MIG-03 | tier1 | K8s 1.18 → 1.22+ (Ingress) | `extensions/v1beta1 Ingress`, `v1 Service` | `networking.k8s.io/v1` (no extensions) | Ingress Incompatible (suggestion: networking.k8s.io/v1), Service OK |
| MIG-04 | tier1 | CRDs not installed | `operators.coreos.com/v1alpha1 Subscription`, `apps/v1 Deployment` | `apps/v1` (no operators.coreos.com) | Subscription Incompatible, Deployment OK |
| MIG-05 | tier2 | Mixed CRDs | `example.com/v1 Widget`, `example.com/v1 Gadget`, `v1 ConfigMap` | `example.com/v1` with widgets only | Widget OK, Gadget Incompatible ("kind not found"), ConfigMap OK |
| MIG-06 | tier2 | Post-transform clean run | All manifests use target-compatible versions | Target serves all | Exit 0, all OK |

---

## 5. End-to-End Tests

Require two minikube clusters (contexts: `src`, `tgt`). **Status: None exist yet — all to add.**

### 5.1 Live Mode — Basic Validation

| ID | Tier | Test Case | Setup | Expected |
|----|------|-----------|-------|----------|
| VLD-001 | tier0 | All-compatible standard resources | Deployment + Service + ConfigMap + Secret YAML against tgt | Exit 0, PASSED, 4 scanned 4 compatible, mode=live |
| VLD-002 | tier0 | Single incompatible resource | `route.openshift.io/v1 Route` against minikube | Exit 1, FAILED, failure file, no suggestion |
| VLD-003 | tier0 | Mixed compatible and incompatible | 2 OK + 2 bad (Route, DeploymentConfig) | Exit 1, 2 compatible 2 incompatible, 2 failure files |
| VLD-004 | tier1 | Suggestion for alternative GV | `extensions/v1beta1 Deployment` | Exit 1, suggestion "available as apps/v1" |

### 5.2 Live Mode — Scanner Edge Cases

| ID | Tier | Test Case | Setup | Expected |
|----|------|-----------|-------|----------|
| VLD-005 | tier1 | Multi-document YAML | 3 docs in one file | Exit 0, 3 scanned |
| VLD-006 | tier1 | Deduplication | Same GVK+ns in two files | Exit 0, totalScanned=1 |
| VLD-007 | tier1 | Nested directory scanning | resources/ns1/ + _cluster/ structure | All found |
| VLD-008 | tier1 | failures/ subdirectory skipped | Incompatible YAML in failures/ | Exit 0, not scanned |
| VLD-009 | tier1 | Core group resources (v1) | Pod, Namespace, PVC, ServiceAccount | Exit 0, correct resourcePlural |
| VLD-010 | tier1 | Cluster-scoped resources | ClusterRole, ClusterRoleBinding | Exit 0, empty namespace |
| VLD-011 | tier1 | YAML report format | `-o yaml` | report.yaml exists, valid YAML |
| VLD-012 | tier2 | Non-YAML files ignored | .txt, .csv, .md alongside .yaml | Only YAML scanned |
| VLD-013 | tier2 | Empty YAML documents | Empty `---` separators | Only valid doc counted |
| VLD-014 | tier2 | Missing apiVersion or kind | Partial metadata docs | Only complete docs counted |
| VLD-015 | tier2 | .json manifest file | JSON-format manifest | Parsed correctly |
| VLD-016 | tier2 | .yml extension | .yml file | Scanned as .yaml |
| VLD-017 | tier2 | Empty input directory | No files | Exit 0, 0 scanned, PASSED |
| VLD-018 | tier1 | Report mode and context fields | Check report JSON | mode=live, clusterContext=tgt |
| VLD-019 | tier2 | Table column headers + mode | Check stdout | All 7 columns + "Mode: live (context: tgt)" |
| VLD-020 | tier2 | Validate-dir auto-created | Non-existent dir | Created, report written |
| VLD-021 | tier2 | Consecutive runs consistent | Two runs same input | Identical reports |

### 5.3 Offline Mode

| ID | Tier | Test Case | Setup | Expected |
|----|------|-----------|-------|----------|
| VLD-030 | tier0 | Basic offline — all compatible | Matching api-resources.json | Exit 0, mode=offline |
| VLD-031 | tier0 | Offline — incompatible resource | GVK not in JSON | Exit 1, failure file |
| VLD-032 | tier1 | Offline — core group (group omitted) | group="" in JSON | Matched as v1 |
| VLD-033 | tier1 | Offline — suggestion | extensions/v1beta1 Deployment, apps/v1 in JSON | suggestion "apps/v1" |
| VLD-034 | tier1 | Offline — malformed JSON | Invalid JSON syntax | Exit 1, parse error |
| VLD-035 | tier1 | Offline — empty resources | resources: [] | Exit 1, "contains no resources" |
| VLD-036 | tier2 | Offline — duplicate kind (Event) | Event in v1 and events.k8s.io/v1 | v1 Event matched correctly |
| VLD-037 | tier1 | Offline — file not found | Non-existent path | Exit 1, file error |
| VLD-038 | tier2 | Offline — YAML report | `-o yaml` | report.yaml with mode=offline |
| VLD-039 | tier2 | Offline — table header | Check stdout | "Mode: offline (api-resources: ...)" |
| VLD-040 | tier2 | Offline — real kubectl output | Capture from tgt cluster | Standard resources compatible |

### 5.4 Cross-Mode Equivalence

| ID | Tier | Test Case | Setup | Expected |
|----|------|-----------|-------|----------|
| VLD-050 | tier1 | Live vs offline identical (compatible) | Same manifests, captured api-resources | Identical counts and statuses |
| VLD-051 | tier1 | Live vs offline identical (incompatible) | Include Route + standard | Same split |
| VLD-052 | tier2 | Cross-mode suggestion equivalence | extensions/v1beta1 Deployment | Same suggestion text |

### 5.5 Flag and CLI Validation

| ID | Tier | Test Case | Expected |
|----|------|-----------|----------|
| VLD-060 | tier0 | NoArgs rejects positional arguments | Exit 1, "unknown command" |
| VLD-061 | tier0 | --api-resources + --context mutually exclusive | Exit 1, "mutually exclusive" |
| VLD-062 | tier0 | --api-resources + --kubeconfig mutually exclusive | Exit 1, "mutually exclusive" |
| VLD-063 | tier1 | Invalid --output format (-o xml) | Exit 1, error about yaml/json |
| VLD-064 | tier1 | Missing input-dir | Exit 1, "input-dir" in error |
| VLD-065 | tier1 | input-dir is a file | Exit 1, "not a directory" |
| VLD-066 | tier2 | Default input-dir is "output" | Exit 1, error references "output" |
| VLD-067 | tier2 | Default validate-dir is "validate" | Results written to ./validate/ |

### 5.6 Edge Cases and Bug-Hunting

| ID | Tier | Test Case | Expected |
|----|------|-----------|----------|
| VLD-070 | tier1 | Bad YAML doc in multi-doc (valid, invalid, valid) | 2 entries parsed, warning logged |
| VLD-071 | tier2 | Failure filename sanitization | Sanitized, no path traversal |
| VLD-072 | tier1 | Subresource filtering (kind=Scale under apps/v1) | "kind Scale not found" |
| VLD-073 | tier2 | Multiple suggestions (Event in v1 and events.k8s.io/v1) | Both alternatives listed, sorted |
| VLD-074 | tier1 | Failures dir cleaned between runs | Second run only has its own failures |
| VLD-075 | tier2 | Cluster-scoped "clusterscoped" in failure filename | `_clusterscoped.yaml` |
| VLD-076 | tier2 | Large multi-doc YAML (500 docs, same GVK) | totalScanned=1, completes <30s |
| VLD-077 | tier2 | CRLF line endings | Parsed correctly |
| VLD-078 | tier2 | Invalid apiVersion format (apps/) | Not rejected by scanner (finding: treated as incompatible) |
| VLD-079 | tier2 | Concurrent validate runs | Both succeed independently |
| VLD-080 | tier2 | Report overwritten on re-run | Second report replaces first |
| VLD-081 | tier1 | GV present but kind missing (apps/v1 DaemonPool) | "kind DaemonPool not found in apps/v1" |

### 5.7 Pipeline Integration

| ID | Tier | Test Case | Steps | Expected |
|----|------|-----------|-------|----------|
| VLD-090 | tier0 | Full pipeline: export→transform→apply→validate | Deploy nginx to src, run full pipeline, validate against tgt | Exit 0, all resources compatible |
| VLD-091 | tier1 | Validate gates apply | Validate output, then kubectl apply to tgt | Validate exit 0, apply succeeds |
| VLD-092 | tier2 | Live-then-offline cross-check after pipeline | Run validate both ways after pipeline | Identical results |

---

## 6. Negative / Edge Case Tests

| ID | Tier | Test Case | Expected |
|----|------|-----------|----------|
| NEG-01 | tier1 | Target cluster unreachable (live mode) | Error: connection refused/timeout, exit 1 |
| NEG-02 | tier1 | Invalid kubeconfig path | Error with descriptive message, exit 1 |
| NEG-03 | tier1 | Kubeconfig context does not exist | Error: context not found, exit 1 |
| NEG-04 | tier2 | Very large export (1000+ manifests) | Completes without OOM or unreasonable delay |
| NEG-05 | tier2 | Read-only filesystem for validate-dir | Error writing report, exit 1 |
| NEG-06 | tier2 | Symlinks in input directory | Follows symlinks to find YAML files |
| NEG-07 | tier2 | Concurrent CRD installation on target | Discovery cache may be stale — known limitation |

---

## 7. Security Tests

| ID | Tier | Test Case | Expected |
|----|------|-----------|----------|
| SEC-01 | tier0 | Read-only cluster access | Validate makes only discovery API calls — no create/update/delete |
| SEC-02 | tier1 | No source file modification | All files in `--input-dir` are byte-identical after validate |
| SEC-03 | tier2 | Kubeconfig not leaked in output | Report and error messages do not include kubeconfig credentials |
| SEC-04 | tier1 | Validate works with minimal RBAC | Any authenticated user (system:discovery) can run validate |
| SEC-05 | tier1 | Path traversal prevention | Malicious manifest content cannot escape validate-dir/failures/ |

---

## 8. Regression Tests

| ID | Tier | Test Case | Expected |
|----|------|-----------|----------|
| REG-01 | tier0 | `crane export` unchanged | Behavior, flags, output identical |
| REG-02 | tier0 | `crane transform` unchanged | Behavior, flags, plugin system identical |
| REG-03 | tier0 | `crane apply` unchanged | Behavior, flags, cluster interaction identical |
| REG-04 | tier1 | Root command help | `crane --help` lists `validate` alongside existing commands |
| REG-05 | tier0 | Existing tests pass | `go test ./...` — all pre-existing tests remain green |
| REG-06 | tier2 | No new dependencies in other commands | Other commands do not import `internal/validate` |

---

## 9. Exit Code Matrix

| Condition | Exit Code | Test IDs |
|-----------|-----------|----------|
| All GVKs compatible | 0 | VLD-001, VLD-017, VLD-030, VLD-090, INT-01, INT-04 |
| One or more incompatible GVKs | 1 | VLD-002, VLD-003, VLD-004, VLD-031, INT-02, INT-03 |
| Runtime error (I/O, connectivity, flags) | 1 | VLD-034, VLD-035, VLD-037, VLD-060–VLD-065, NEG-01–NEG-03 |

---

## 10. Test Coverage Summary

### Current State (36 tests implemented)

| Component | Existing Tests | Coverage |
|-----------|---------------|----------|
| Scanner | 9 tests | Core paths covered: single-doc, multi-doc, nested dirs, dedup, failures skip, non-YAML, core/named groups, cluster-scoped |
| Matcher (live) | 8 tests | Core paths covered: all OK, missing GV, missing kind, mixed, suggestions, no suggestion, empty, resource plural |
| Matcher (offline via index) | 3 tests | Basic paths: all OK, incompatible, suggestion |
| API Resources Parser | 9 tests | Well covered: valid, core, non-core, plural, empty, malformed, file not found, duplicate kinds, namespaced |
| Reporter | 4 tests | Partial: table mixed, table empty, JSON, JSON round-trip |
| Command | 3 test functions (12 sub-tests) | Well covered: flags, defaults, validation, mutual exclusion, NoArgs |
| **Integration** | **0** | **Not started** |
| **E2E** | **0** | **Not started** |

### To Add

| Category | Count | tier0 | tier1 | tier2 | Priority |
|----------|-------|-------|-------|-------|----------|
| Scanner unit tests | 11 | 0 | 4 | 7 | Medium |
| Matcher unit tests | 6 | 0 | 2 | 4 | Medium |
| Reporter unit tests | 8 | 0 | 3 | 5 | Medium |
| Integration tests | 18 | 2 | 10 | 6 | High |
| E2E — live mode | 21 | 3 | 8 | 10 | High |
| E2E — offline mode | 11 | 2 | 5 | 4 | High |
| E2E — cross-mode | 3 | 0 | 2 | 1 | Medium |
| E2E — flags/CLI | 8 | 3 | 3 | 2 | Medium |
| E2E — edge cases | 12 | 0 | 4 | 8 | Medium |
| E2E — pipeline | 3 | 1 | 1 | 1 | High |
| Negative tests | 7 | 0 | 3 | 4 | Low |
| Security tests | 5 | 1 | 3 | 1 | Medium |
| Regression tests | 6 | 4 | 1 | 1 | Low |

### Tier Distribution (all tests — existing + to add)

| Tier | Existing | To Add | Total |
|------|----------|--------|-------|
| tier0 | 19 | 16 | 35 |
| tier1 | 12 | 48 | 60 |
| tier2 | 5 | 54 | 59 |
| **Total** | **36** | **118** | **154** |

---

## 11. Test Infrastructure Requirements

### Unit & Integration
- Go standard `testing` package
- `genericclioptions.NewTestIOStreamsDiscard()` for IO streams
- Mock `discovery.DiscoveryInterface` (existing pattern from `cmd/export/discover_test.go`)
- Temporary directories via `t.TempDir()` for manifest fixtures
- Offline tests use crafted `api-resources.json` files — no cluster needed

### E2E
- Two minikube clusters with contexts `src` and `tgt`
- `crane` binary built from current branch
- `kubectl` available in PATH
- `CraneRunner.Validate()` method to add to `e2e-tests/framework/crane.go`
- Helper utilities: `WriteTestManifest()`, `WriteAPIResourcesJSON()`, `ParseValidateReport()`
- `DeferCleanup` for temp directory and namespace cleanup

### Running Tests

```bash
# Unit tests
go test ./internal/validate/... ./cmd/validate/... -v -count=1

# E2E — all validate tests
ginkgo run -v --focus="VLD-" e2e-tests/tests -- \
  --crane-bin=./crane --source-context=src --target-context=tgt

# E2E — tier0 only (gate tests)
ginkgo run -v --label-filter="tier0" --focus="VLD-" e2e-tests/tests -- \
  --crane-bin=./crane --source-context=src --target-context=tgt

# E2E — tier0 + tier1 (standard coverage)
ginkgo run -v --label-filter="tier0 || tier1" --focus="VLD-" e2e-tests/tests -- \
  --crane-bin=./crane --source-context=src --target-context=tgt
```

---

## 12. Traceability to Acceptance Criteria

| Acceptance Criterion (from issue #230) | Test IDs |
|----------------------------------------|----------|
| Accepts `--input-dir` + target `--kubeconfig`/`--context` | CMD-01, CMD-02, VLD-001, VLD-018 |
| Accepts `--api-resources` for offline mode (mutually exclusive) | CMD-08–CMD-11, VLD-030–VLD-040, VLD-061, VLD-062 |
| Scans YAML, extracts GVK tuples, checks against target discovery | S-01–S-09, M-01–M-08, VLD-005–VLD-016 |
| Table always printed to terminal | R-01, VLD-019, INT-11 |
| Report file format controlled by `-o json/yaml` | R-03, VLD-011, VLD-038, INT-09, INT-10 |
| Suggests alternative GVs when available | M-05, VLD-004, VLD-033, VLD-052, VLD-073 |
| Failure artifacts written as YAML | VLD-002, VLD-003, VLD-071, VLD-075, INT-08 |
| Deterministic exit codes: 0/1 | Exit Code Matrix (Section 9) |
| Read-only: no modifications | SEC-01, SEC-02 |
| Low privilege: discovery API only | SEC-04 |
| Live and offline produce equivalent results | VLD-050–VLD-052, VLD-092 |
| Pipeline: export → transform → apply → validate | VLD-090, VLD-091 |
| 36 unit tests (scanner, matcher, api-resources, reporter, command) | S-*, M-*, A-*, R-*, CMD-* |
