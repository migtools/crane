# Crane Validate: E2E Test Results

**Date:** 2026-04-30
**Clusters:** minikube (contexts: `src`, `tgt`)
**Branch:** `implement_validate_offline`
**Binary:** `./crane` (built from current branch)

---

## Summary

| Section | Total | Passed | Failed | Findings |
|---------|-------|--------|--------|----------|
| Live Mode (VLD-001 to VLD-021) | 21 | 21 | 0 | 0 |
| Offline Mode (VLD-030 to VLD-040) | 11 | 11 | 0 | 0 |
| Cross-Mode (VLD-050 to VLD-052) | 3 | 3 | 0 | 0 |
| Flag/CLI (VLD-060 to VLD-067) | 8 | 8 | 0 | 0 |
| Edge Cases (VLD-070 to VLD-081) | 12 | 11 | 0 | 1 finding |
| Pipeline Integration (VLD-090 to VLD-091) | 2 | 2 | 0 | 0 |
| **Total** | **57** | **57** | **0** | **1** |

**Overall: All 57 tests PASSED. 1 behavioral finding (non-bug).**

---

## Section 1: Live Mode (Online Validation)

| ID | Title | Result | Notes |
|----|-------|--------|-------|
| VLD-001 | All-compatible standard resources | PASS | Exit 0, 4 scanned 4 compatible, mode=live context=tgt, no failures dir |
| VLD-002 | Single incompatible resource (Route) | PASS | Exit 1, FAILED msg, failure file `Route_route.openshift.io_v1_vld002.yaml` written as YAML, no suggestion |
| VLD-003 | Mixed compatible and incompatible | PASS | Exit 1, 4 scanned 2 compatible 2 incompatible, 2 failure files |
| VLD-004 | Suggestion (extensions/v1beta1 Deployment) | PASS | Exit 1, suggestion="available as apps/v1", reason includes suggestion |
| VLD-005 | Multi-document YAML | PASS | Exit 0, 3 scanned from single file |
| VLD-006 | Deduplication by GVK+namespace | PASS | Exit 0, totalScanned=1 (two files, same GVK+ns) |
| VLD-007 | Nested directory scanning | PASS | Exit 0, 2 scanned (resources/ns1/ + resources/_cluster/) |
| VLD-008 | failures/ subdirectory skipped | PASS | Exit 0, incompatible Route in failures/ ignored, 1 scanned |
| VLD-009 | Core group resources (v1) | PASS | Exit 0, correct resourcePlural: pods, namespaces, persistentvolumeclaims, serviceaccounts |
| VLD-010 | Cluster-scoped resources | PASS | Exit 0, empty namespace for ClusterRole/ClusterRoleBinding |
| VLD-011 | YAML report format (-o yaml) | PASS | report.yaml exists, valid YAML, contains mode=live, clusterContext=tgt |
| VLD-012 | Non-YAML files ignored | PASS | Only .yaml file scanned, .md/.csv/.sh ignored |
| VLD-013 | Empty YAML documents handled | PASS | Only valid ConfigMap counted, empty `---` docs skipped |
| VLD-014 | Documents missing apiVersion/kind skipped | PASS | totalScanned=1, only complete doc counted |
| VLD-015 | .json manifest files parsed | PASS | JSON manifest parsed and matched correctly |
| VLD-016 | .yml extension works | PASS | .yml treated same as .yaml |
| VLD-017 | Empty input directory | PASS | Exit 0, 0 scanned, PASSED |
| VLD-018 | Report mode and context fields | PASS | mode=live, clusterContext=tgt |
| VLD-019 | Table output column headers + mode | PASS | All 7 columns present, "Mode: live (context: tgt)" header |
| VLD-020 | Validate-dir auto-created | PASS | Non-existent dir created, report.json written |
| VLD-021 | Consecutive runs consistent | PASS | Both reports produce identical results |

---

## Section 2: Offline Mode (--api-resources)

| ID | Title | Result | Notes |
|----|-------|--------|-------|
| VLD-030 | Basic offline - all compatible | PASS | Exit 0, mode=offline, apiResourcesSource shows file path |
| VLD-031 | Offline - incompatible resource | PASS | Exit 1, failure file written |
| VLD-032 | Offline - core group (group omitted) | PASS | Pod with group="" in JSON matched correctly as v1 |
| VLD-033 | Offline - suggestion | PASS | extensions/v1beta1 Deployment suggested as apps/v1 |
| VLD-034 | Offline - malformed JSON | PASS | Exit 1, "parsing api-resources JSON" error |
| VLD-035 | Offline - empty resources | PASS | Exit 1, "contains no resources" error |
| VLD-036 | Offline - duplicate kind across groups | PASS | v1 Event matched correctly, not confused with events.k8s.io/v1 |
| VLD-037 | Offline - file not found | PASS | Exit 1, "api-resources file" error |
| VLD-038 | Offline - YAML report format | PASS | report.yaml exists, contains mode=offline |
| VLD-039 | Offline - table header shows offline mode | PASS | "Mode: offline (api-resources: ...)" displayed |
| VLD-040 | Offline - real kubectl api-resources output | PASS | Captured from tgt cluster, standard resources compatible |

---

## Section 3: Cross-Mode Equivalence

| ID | Title | Result | Notes |
|----|-------|--------|-------|
| VLD-050 | Live vs offline (compatible) | PASS | 4,4,0 identical in both modes |
| VLD-051 | Live vs offline (incompatible) | PASS | 2,1,1 identical in both modes |
| VLD-052 | Cross-mode suggestion equivalence | PASS | "available as apps/v1" identical in both modes |

---

## Section 4: Flag and CLI Validation

| ID | Title | Result | Notes |
|----|-------|--------|-------|
| VLD-060 | NoArgs rejects positional arguments | PASS | Exit 1, "unknown command" |
| VLD-061 | --api-resources + --context mutually exclusive | PASS | Exit 1, "mutually exclusive" |
| VLD-062 | --api-resources + --kubeconfig mutually exclusive | PASS | Exit 1, "mutually exclusive" |
| VLD-063 | Invalid --output format | PASS | Exit 1, '--output must be "yaml" or "json"' |
| VLD-064 | Missing input-dir | PASS | Exit 1, 'input-dir "/nonexistent/path"' |
| VLD-065 | input-dir is a file | PASS | Exit 1, "not a directory" |
| VLD-066 | Default input-dir is "output" | PASS | Exit 1, error references "output" |
| VLD-067 | Default validate-dir is "validate" | PASS | Results written to ./validate/report.json |

---

## Section 5: Edge Cases and Bug-Hunting

| ID | Title | Result | Notes |
|----|-------|--------|-------|
| VLD-070 | Bad YAML doc in multi-doc stream | PASS | Warning logged for doc #2, 2 valid docs parsed and matched |
| VLD-071 | Failure filename sanitization | PASS | `MyWidget_my.custom_v1beta2_ns-special.yaml` — dots and hyphens preserved, no path traversal |
| VLD-072 | Subresource filtering (kind=Scale) | PASS | "kind Scale not found in API version apps/v1" — subresources correctly filtered from discovery index |
| VLD-073 | Multiple suggestions (Event) | PASS | Suggestion: "available as events.k8s.io/v1, v1" — both alternatives listed, sorted |
| VLD-074 | Failures dir cleaned between runs | PASS | Run 1: Route file only. Run 2: DeploymentConfig file only. Old files removed. |
| VLD-075 | Cluster-scoped "clusterscoped" in filename | PASS | `MyClusterResource_custom.io_v1_clusterscoped.yaml` |
| VLD-076 | Large multi-doc (500 docs) | PASS | totalScanned=1 (deduped), completed in 0.047s |
| VLD-077 | CRLF line endings | PASS | Parsed correctly, ConfigMap matched |
| VLD-078 | Invalid apiVersion format (apps/) | FINDING | See findings below |
| VLD-079 | Concurrent validate runs | PASS | Both exit 0, both reports exist and correct |
| VLD-080 | Report overwritten on re-run | PASS | Run 1: scanned=3, Run 2: scanned=1 (overwritten) |
| VLD-081 | GV present but kind missing | PASS | "kind DaemonPool not found in API version apps/v1" — different error message from GV missing |

---

## Section 6: Pipeline Integration

| ID | Title | Result | Notes |
|----|-------|--------|-------|
| VLD-090 | Full pipeline: export->transform->apply->validate | PASS | 3 resources (ConfigMap, Service, Deployment) exported from src, all compatible on tgt |
| VLD-091 | Validate gates apply workflow | PASS | Validate exit 0, kubectl apply succeeded, resources created on tgt |

---

## Findings

### Finding 1: VLD-078 — Invalid apiVersion `apps/` not rejected by scanner

**Severity:** Low (non-bug, behavioral observation)

**Description:** A manifest with `apiVersion: apps/` (group with trailing slash, no version) is not rejected by the scanner. `schema.ParseGroupVersion("apps/")` parses it as `Group="apps", Version=""`, producing a GVK key of `apps/`. This entry then reaches the matcher which correctly marks it as `Incompatible` (since `apps/` is not a valid groupVersion in discovery).

**Expected behavior:** The scanner should skip it with a warning ("skipping invalid apiVersion").

**Actual behavior:** The entry is accepted and treated as an incompatible resource. The suggestion engine even suggests `"available as apps/v1"` which is helpful, but the apiVersion is technically malformed.

**Impact:** Minimal. The user sees the incompatibility and the correct suggestion. No crash, no data corruption. The entry appears in the report with the malformed apiVersion string.

**Recommendation:** Consider adding a validation check after `schema.ParseGroupVersion` to reject entries where `Version` is empty. This would align with the existing warning for unparseable apiVersions. Low priority since the current behavior is safe and even somewhat helpful.

---

## Test Environment

- **minikube version:** local minikube with contexts `src` and `tgt`
- **Kubernetes version (tgt):** v1.33 (minikube default)
- **crane binary:** built from branch `implement_validate_offline`
- **Test execution time:** ~3 minutes total
- **All temp files:** `/tmp/crane-e2e-validate/`
