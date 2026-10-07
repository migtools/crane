# `crane validate` Command -- Test Results

**Date:** 2026-04-21
**Environment:** Two minikube clusters (profiles `src` and `tgt`), Kubernetes v1.34.0, macOS arm64
**Binary:** `./crane` built from current HEAD

---

## Summary

| Category | Tests | Passed | Failed | Bugs |
|----------|-------|--------|--------|------|
| A: Basic Happy Path | 3 | 3 | 0 | 0 |
| B: Incompatible Resources | 3 | 3 | 0 | 0 |
| F: Suggestion Hints | 4 | 4 | 0 | 0 |
| C: Scanner Edge Cases | 7 | 7 | 0 | 0 |
| D: Error Paths | 5 | 5 | 0 | 0 |
| E: Bug Probing | 7 | 5 | 1 | 1 |
| **Total** | **29** | **27** | **1** | **1** |

**1 skipped** (TC-24: partial discovery failure -- not simulatable on minikube without API server modification).

---

## Bugs Found

### BUG-1: Malformed YAML mid-file skips all remaining documents (TC-20)

**Severity:** Medium
**File:** `internal/validate/scanner.go`, line ~68 (`break` on decode error)

**Description:** In a multi-document YAML file, if the second document is malformed, the scanner logs a warning and `break`s out of the decode loop. This causes all subsequent valid documents in the same file to be silently skipped.

**Reproduction:**
```yaml
# File: malformed.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: first
  namespace: default
---
this is not valid yaml: [[[
  bad: {unclosed
---
apiVersion: v1
kind: Secret
metadata:
  name: third
  namespace: default
```

**Expected:** ConfigMap and Secret both scanned (2 entries).
**Actual:** Only ConfigMap scanned (1 entry). Secret silently lost.

**Fix:** Change `break` to `continue` in the decode error handler so the scanner attempts to parse subsequent documents.

---

## Observations (Non-Bugs)

### OBS-1: Cobra prints usage on RunE errors

When `RunE` returns an error (e.g. `ErrValidationFailed`), Cobra prints the full command usage alongside the error message. This makes the error output very noisy. Consider setting `cmd.SilenceUsage = true` on the validate command to suppress the usage dump on expected errors.

---

## Detailed Results

### Category A: Basic Happy Path

| TC | Description | Result | Notes |
|----|-------------|--------|-------|
| TC-01 | Valid export, all GVKs on target | **PASS** | 8 GVKs scanned, all OK, exit 0, SUGGESTION empty |
| TC-02 | JSON output format | **PASS** | Valid JSON, 8 compatible, no suggestion fields, exit 0 |
| TC-03 | Scan export-dir + output-dir | **PASS** | Both dirs scanned, deduplication correct, all OK |

### Category B: Incompatible Resources

| TC | Description | Result | Notes |
|----|-------------|--------|-------|
| TC-04 | OpenShift Route (no alternative) | **PASS** | Incompatible, no suggestion, exit 1 |
| TC-05 | Valid group, missing kind | **PASS** | "kind FakeResource not found in API version apps/v1", no suggestion |
| TC-06 | Mixed OK + incompatible | **PASS** | Deployment OK, Widget Incompatible, exit 1 |

### Category F: Suggestion / Alternative GV Hints

| TC | Description | Result | Notes |
|----|-------------|--------|-------|
| TC-26 | Deprecated GV (extensions/v1beta1 Deployment) -- table | **PASS** | Reason: "(available as apps/v1)", SUGGESTION column populated |
| TC-27 | Same as TC-26 in JSON | **PASS** | `"suggestion": "available as apps/v1"` in JSON output |
| TC-28 | Multiple alternatives (Event) | **PASS** | Suggestion: "available as events.k8s.io/v1, v1" (sorted) |
| TC-29 | Exact match exists, no false suggestion | **PASS** | OK status, no suggestion |

### Category C: Scanner Edge Cases

| TC | Description | Result | Notes |
|----|-------------|--------|-------|
| TC-07 | Multi-document YAML | **PASS** | Both ConfigMap and Secret scanned from single file |
| TC-08 | Empty / comments-only YAML | **PASS** | 0 scanned, exit 0 |
| TC-09 | Non-YAML files ignored | **PASS** | .txt, .md, .csv ignored; only .yaml processed |
| TC-10 | `failures/` directory skipped | **PASS** | Route in failures/ not scanned |
| TC-11 | Nested directories | **PASS** | Both `resources/ns/` and `_cluster/` levels scanned |
| TC-12 | Cluster-scoped resource | **PASS** | Empty namespace column, ClusterRole validated OK |
| TC-13 | Deduplication across files | **PASS** | 2 files with same GVK+ns -> 1 entry |

### Category D: Error Paths

| TC | Description | Result | Notes |
|----|-------------|--------|-------|
| TC-14 | Non-existent export-dir | **PASS** | `export-dir "/nonexistent/path": ... no such file or directory` |
| TC-15 | export-dir is a file | **PASS** | `export-dir "..." is not a directory` |
| TC-16 | Invalid output format (-o xml) | **PASS** | `--output must be "table" or "json", got "xml"` |
| TC-17 | Invalid kubeconfig | **PASS** | Error during Complete(), exit 1 |
| TC-18 | Empty directory | **PASS** | 0 scanned, exit 0 |

### Category E: Bug Probing

| TC | Description | Result | Notes |
|----|-------------|--------|-------|
| TC-19 | globalFlags nil panic | **PASS** | No panic; viper creates zero-value struct |
| TC-20 | Malformed YAML mid-file | **BUG** | Third valid doc skipped after second doc's parse error. See BUG-1 |
| TC-21 | apiVersion without kind | **PASS** | Silently skipped |
| TC-22 | kind without apiVersion | **PASS** | Silently skipped |
| TC-23 | JSON manifest (.json file) | **PASS** | Scanned correctly |
| TC-24 | Partial discovery failure | **SKIP** | Not simulatable on minikube |
| TC-25 | Large directory (200 files) | **PASS** | 200 files -> 10 deduplicated entries, 77ms |
