# Crane Export Command — Bug Hunting Test Plan

## Setup Prerequisites

```bash
# Two minikube clusters
minikube start -p src
minikube start -p tgt

# Build crane
go build -o crane .
```

---

## Phase 1: Flag & Input Validation

| # | Test | Command | What to Watch |
|---|------|---------|---------------|
| 1.1 | Default namespace (from kubeconfig context) | `./crane export` | Should use current context's namespace |
| 1.2 | Explicit namespace | `./crane export -n kube-system` | Works with system namespaces |
| 1.3 | Empty namespace string | `./crane export -n ""` | Should error cleanly |
| 1.4 | Non-existent namespace | `./crane export -n does-not-exist` | Should error (NotFound) |
| 1.5 | Custom export dir | `./crane export -e /tmp/crane-test` | Creates dir at path |
| 1.6 | Export dir is a file | `touch /tmp/afile && ./crane export -e /tmp/afile` | Error or overwrite? |
| 1.7 | Export dir with no write perms | `mkdir /tmp/noperm && chmod 000 /tmp/noperm && ./crane export -e /tmp/noperm/out` | Clean error? |
| 1.8 | Relative vs absolute export path | `./crane export -e ./relative/path` | Works correctly? |
| 1.9 | Export dir with spaces | `./crane export -e "/tmp/my export dir"` | Path handling |
| 1.10 | Export dir with special chars | `./crane export -e "/tmp/export-$(date)"` | Shell expansion in path |
| 1.11 | Re-export to same dir | Run export twice to same `-e` dir | Overwrites? Merges? Stale `_cluster/` cleaned? |

---

## Phase 2: Label Selectors

| # | Test | Command | What to Watch |
|---|------|---------|---------------|
| 2.1 | Valid equality selector | `./crane export -l app=nginx` | Only matching resources |
| 2.2 | Valid set-based selector | `./crane export -l 'app in (nginx,redis)'` | Correct filtering |
| 2.3 | Invalid selector syntax | `./crane export -l 'app in (unclosed'` | Clean error in Validate |
| 2.4 | Selector matching nothing | `./crane export -l app=nonexistent` | Empty export, no error? |
| 2.5 | Selector with special chars | `./crane export -l 'app.kubernetes.io/name=test'` | Label key with dots/slashes |
| 2.6 | Multiple selectors | `./crane export -l 'app=nginx,tier=frontend'` | AND logic |
| 2.7 | NotIn selector | `./crane export -l 'app notin (redis)'` | Exclusion works? |
| 2.8 | Label selector + cluster-scoped | `./crane export -l app=nginx` | Are RBAC/CRDs also filtered by label, or only namespaced resources? (potential bug: labels applied to List for cluster-scoped too) |

---

## Phase 3: Kubeconfig & Context Handling

| # | Test | Command | What to Watch |
|---|------|---------|---------------|
| 3.1 | Explicit context | `./crane export --context src` | Uses correct cluster |
| 3.2 | Non-existent context | `./crane export --context bogus` | Clean error |
| 3.3 | Custom kubeconfig | `./crane export --kubeconfig /path/to/config` | Works |
| 3.4 | Missing kubeconfig file | `./crane export --kubeconfig /nonexistent` | Clean error |
| 3.5 | No default namespace in context | Set context with no namespace, run `./crane export` | What happens? |
| 3.6 | Multiple kubeconfig merge | `KUBECONFIG=a:b ./crane export` | Standard merge behavior |

---

## Phase 4: Resource Types — Simple Apps

Deploy these and export:

| # | App | Resources Created | What to Watch |
|---|-----|-------------------|---------------|
| 4.1 | nginx Deployment | Deployment, ReplicaSet, Pod, Service, SA | Basic namespaced export |
| 4.2 | StatefulSet (e.g. redis) | StatefulSet, PVC, PV, Service (headless) | PVC/PV handling |
| 4.3 | Job + CronJob | Job, CronJob, Pods | Completed/failed pods |
| 4.4 | DaemonSet | DaemonSet, Pods on each node | DaemonSet export |
| 4.5 | ConfigMap + Secret | ConfigMap, Secret | Secret export (sensitive data!) |
| 4.6 | Ingress | Ingress, Service | Ingress resource |
| 4.7 | HPA | HPA, Deployment | autoscaling group |
| 4.8 | PDB | PodDisruptionBudget | policy group |
| 4.9 | NetworkPolicy | NetworkPolicy | networking group |
| 4.10 | ServiceAccount with RBAC | SA, Role, RoleBinding, ClusterRole, ClusterRoleBinding | RBAC chain |

---

## Phase 5: Complex / Weird Scenarios

| # | Scenario | Setup | What to Watch |
|---|----------|-------|---------------|
| 5.1 | Resource with very long name (253 chars) | Create ConfigMap with max-length name | Filename truncation? Path too long? |
| 5.2 | Resource name with dots/special chars | `kubectl create cm my.config.v2 --from-literal=a=b` | Filename generation |
| 5.3 | Namespace with special chars | Create namespace `test-ns.v2` or `test_ns` | Path handling |
| 5.4 | Empty namespace (no resources) | `kubectl create ns empty && ./crane export -n empty` | Empty export dir, no error |
| 5.5 | Namespace with 1000+ resources | Script-create many ConfigMaps | Performance, pagination, memory |
| 5.6 | Resource with large data (1MB ConfigMap) | `kubectl create cm big --from-file=bigfile` | File write handling |
| 5.7 | Resource with binary data in Secret | Create Secret with binary content | YAML serialization |
| 5.8 | Two SAs, one with RBAC one without | Export namespace with mixed RBAC | Only relevant ClusterRoles exported |
| 5.9 | ClusterRoleBinding referencing SA in OTHER namespace | CRB subjects include SA from non-exported ns | Should NOT be exported |
| 5.10 | ClusterRoleBinding with Group subject | CRB with `system:serviceaccounts:<ns>` | Should be caught by group matching |
| 5.11 | ClusterRoleBinding with User subject | CRB with `system:serviceaccount:<ns>:<sa>` | User-format SA matching |
| 5.12 | Orphaned ClusterRole (no CRB) | ClusterRole exists but no binding | Should NOT be exported |
| 5.13 | CRD for custom resource | Install a CRD, create CR in namespace | CRD exported alongside CR |
| 5.14 | CRD managed by operator (OLM) | CRD with `olm.managed=true` label | Should be SKIPPED with warning |
| 5.15 | CRD with `app.kubernetes.io/managed-by` label | CRD with managed-by label | Should be SKIPPED |
| 5.16 | CRD in built-in group | | Skipped by default, `--crd-include-group` overrides |
| 5.17 | `--crd-skip-group` flag | `./crane export --crd-skip-group mygroup.io` | Custom group skipped |
| 5.18 | Resource with ownerReferences | Pod owned by ReplicaSet | Both exported? Owner chain? |
| 5.19 | Finalizers on resource | Resource stuck in terminating with finalizer | Export while terminating |
| 5.20 | Resource with very long labels/annotations | Annotations > 256KB | YAML output correctness |

---

## Phase 6: Impersonation

| # | Test | Command | What to Watch |
|---|------|---------|---------------|
| 6.1 | Impersonate user | `./crane export --impersonate user@example.com -n default` | Works with existing RBAC |
| 6.2 | Impersonate group | `./crane export --impersonate-group system:authenticated -n default` | Group impersonation |
| 6.3 | as-extras without impersonate | `./crane export --as-extras "key=val"` | Should error in Validate |
| 6.4 | Malformed as-extras | `./crane export --impersonate admin --as-extras "noequals"` | Parse error |
| 6.5 | as-extras with semicolons | `./crane export --impersonate admin --as-extras "k1=v1,v2;k2=v3"` | Multi-key parsing |
| 6.6 | Impersonate user with no RBAC | `./crane export --impersonate nobody` | Forbidden errors in failures/ |

---

## Phase 7: API Throttling

| # | Test | Command | What to Watch |
|---|------|---------|---------------|
| 7.1 | Very low QPS | `./crane export -q 1 -b 1` | Slow but succeeds |
| 7.2 | Zero QPS | `./crane export -q 0` | Error or hangs? |
| 7.3 | Negative QPS | `./crane export -q -1` | Error handling |
| 7.4 | Very high burst | `./crane export -b 999999` | Works? |

---

## Phase 8: Failure Directory & Error Handling

| # | Test | Command | What to Watch |
|---|------|---------|---------------|
| 8.1 | RBAC-restricted export | Create ServiceAccount with limited RBAC, impersonate it | Forbidden resources → failures/ dir |
| 8.2 | Check failures/ content format | After 8.1, inspect YAML in failures/ | Resource metadata + error message present |
| 8.3 | Re-export clears old failures | Export with failures, fix RBAC, re-export | Old failures/ cleaned |
| 8.4 | Cluster API partially down | (hard to simulate) or CRD group unreachable | Partial discovery warning, continues |
| 8.5 | Disk full during export | Fill /tmp, export to /tmp | Error handling on write |

---

## Phase 9: Output Validation

| # | Check | How | What to Watch |
|---|-------|-----|---------------|
| 9.1 | Filename format | `ls export/resources/<ns>/` | `Kind_group_version_namespace_name.yaml` pattern |
| 9.2 | Cluster-scoped dir | `ls export/resources/<ns>/_cluster/` | Only when cluster-scoped resources exist |
| 9.3 | YAML validity | `kubectl apply --dry-run=client -f <file>` on each | All files valid YAML |
| 9.4 | Resource completeness | Compare `kubectl get all -n <ns>` with export | Nothing missing (except Events) |
| 9.5 | No server-managed fields | Check for `managedFields`, `resourceVersion`, `uid` | Should still be present (stripping is transform phase) |
| 9.6 | File permissions | `stat export/resources/` | `0700` dirs, reasonable file perms |
| 9.7 | Idempotent re-export | Export twice, diff | Identical output (modulo resourceVersion changes) |

---

## Phase 10: Debug Flag & Observability

| # | Test | Command | What to Watch |
|---|------|---------|---------------|
| 10.1 | Debug mode | `./crane export --debug` | Verbose logging, resource-by-resource |
| 10.2 | No debug (default) | `./crane export` | Clean output, no noise |
| 10.3 | Debug shows skipped resources | Export with debug, check for Event skip message | `skipping extracting events` logged |
| 10.4 | Debug shows RBAC filtering | Export with SAs + RBAC + debug | CR/CRB acceptance/rejection logged |

---

## Execution Order Recommendation

Start with **Phase 1** (flags) and **Phase 4** (simple apps) — these give quick coverage and catch obvious issues. Then move to **Phase 5** (weird scenarios) which is where the real bugs hide. Phases 2, 3, 6 are important but more mechanical. Phase 8 (failures) is critical for understanding error reporting quality.

## Known Code Observations (from code review)

- Events are hardcoded to skip (no flag) — `discover.go` line 231
- `imagestreamtags`/`imagetags` use per-item Get instead of List (OpenShift-specific)
- `context.Background()` used in all API calls — no user cancellation support
- O(n*m) linear search in ClusterRole acceptance checking — `cluster.go` line 267
- Operator-managed CRDs silently skipped based on labels/annotations
- Partial discovery errors logged as warnings, may hide real issues
