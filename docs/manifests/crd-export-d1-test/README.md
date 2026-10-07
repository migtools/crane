# CRD export (D1) — real-world cluster test

This directory supports the manual test flow for **exporting CRDs** alongside a **mixed app** (namespaced workload + cluster RBAC, optional OpenShift SCC). It reuses manifests from [`../crane-complex-demo/`](../crane-complex-demo/).

## Custom API reference (Phase A4 / B)

| Field | Value |
|--------|--------|
| Namespace | `crane-complex-demo` |
| Group | `stable.example.com` (not `*.openshift.io` — CRD export applies) |
| Version | `v1` |
| Resource (plural) | `widgets` |
| Kind | `Widget` |
| CRD object name | `widgets.stable.example.com` |

## Prerequisites

- **`kubectl`** or **`oc`** with a context that can create namespaces, CRDs, and (for full test) ClusterRole/ClusterRoleBinding.
- **RBAC:** list/watch/get in the namespace; **get** on `customresourcedefinitions` for D1; list cluster RBAC when using `--cluster-scoped-rbac`.
- Build **crane** from repo root: `go build -o crane .`

## Phase A — Install the mixed app

From repo root (or this directory):

```bash
chmod +x install-demo.sh verify-export.sh
./install-demo.sh
```

This applies:

1. Namespace `crane-complex-demo`
2. CRD `widgets.stable.example.com`
3. **OpenShift only:** `01-scc.yaml` from crane-complex-demo (skipped on plain Kubernetes)
4. ServiceAccount, ConfigMap, Widget CR, ClusterRole, ClusterRoleBinding, Deployment, Service
5. Optional dummy **Secret** (`03-secret-dummy.yaml`)

## Phase B — Baseline inspection

```bash
kubectl api-resources --api-group=stable.example.com -o wide
kubectl get widgets -n crane-complex-demo
kubectl get crd widgets.stable.example.com -o yaml | head
```

## Phase C — Export and verify

```bash
./verify-export.sh /path/to/export-crd-test
# or: CRANE_BIN=/tmp/crane CRANE_EXPORT_NS=crane-complex-demo ./verify-export.sh
```

Or manually:

```bash
./crane export --export-dir ./export-crd-test -n crane-complex-demo --cluster-scoped-rbac
```

**Expect** under `resources/crane-complex-demo/_cluster/`:

- `CustomResourceDefinition_*` for `widgets.stable.example.com`
- `ClusterRole_*` and `ClusterRoleBinding_*` tied to `app-sa`
- On OpenShift (if SCC applied): `SecurityContextConstraints_*` when the export filter includes it

**Expect** under `resources/crane-complex-demo/` (not `_cluster/`): Deployment, Service, ConfigMap, ServiceAccount, Widget, Secret, etc.

## Phase D — `*.openshift.io` skip (optional)

Create or use a CR whose `apiVersion` group ends in `.openshift.io`. Crane **does not** GET a CRD for those groups. The sample **Widget** uses `stable.example.com` specifically so D1 **does** run.

## Phase E — CRD GET forbidden (optional)

Run export as a user **without** `get` on `customresourcedefinitions`. Expect a warn log and `failures/crane-complex-demo/customresourcedefinition-widgets.stable.example.com.yaml`.

## Phase F — Dedupe (optional)

Scale or add a second `Widget` in the same namespace; re-export. Expect **one** CRD file for `widgets.stable.example.com` in `_cluster/`.

## Cleanup (cluster-admin)

```bash
kubectl delete -f ../crane-complex-demo/02-namespace-app.yaml --ignore-not-found
kubectl delete -f 03-secret-dummy.yaml --ignore-not-found
kubectl delete namespace crane-complex-demo --ignore-not-found
kubectl delete -f ../crane-complex-demo/01-scc.yaml --ignore-not-found
kubectl delete -f ../crane-complex-demo/00-crd-widget.yaml --ignore-not-found
```

## Notes

- Discovery only lists types that support **list, create, get, delete**; the sample CRD schema is minimal OpenAPI v3.
- If your branch creates `_cluster/` by default without `-c`, you can omit `--cluster-scoped-rbac` only if CRD export still has a writable `_cluster/` path (see main export code).
