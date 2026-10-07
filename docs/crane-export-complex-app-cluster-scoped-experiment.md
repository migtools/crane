# Complex app + dual-role `crane export --cluster-scoped-rbac` experiments

This document records two exports of the same **composed** application in namespace **`crane-complex-demo`**: once as **cluster admin** (`kube:admin`) and once as a **namespace admin** OAuth user (`crane-complex-user` with `clusterrole=admin` scoped only to that namespace). Both runs used **`crane export --cluster-scoped-rbac`** (`-c`).

Related product context: [migtools/crane#186 — Update export action with cluster-level dependencies](https://github.com/migtools/crane/issues/186).

## Environment

| Item | Value |
|------|--------|
| Crane repo / binary | Built from workspace at git `4d6185f` (adjust if you reproduce on another commit) |
| OpenShift server | `api.oadp-5511.qe.devcluster.openshift.com:6443` (example QE cluster) |
| OpenShift version (server) | Kubernetes `v1.29.14+…`; OpenShift **4.16.58** (`oc version`) |
| Namespace | `crane-complex-demo` |
| Namespace admin user | `crane-complex-user` / `P@ssWord` (htpasswd), `RoleBinding` → `clusterrole=admin` in `crane-complex-demo` only |
| Namespace admin kubeconfig | `/tmp/crane-complex-user-kubeconfig` (dedicated file; current context = namespace user) |

## Application inventory (reproducible manifests)

Manifests live under [`manifests/crane-complex-demo/`](manifests/crane-complex-demo/README.md) (from repo root: `docs/manifests/crane-complex-demo/`). Apply order is documented in that folder’s `README.md`.

| Resource | API / scope | Purpose |
|----------|-------------|---------|
| `CustomResourceDefinition` `widgets.stable.example.com` | `apiextensions.k8s.io/v1`, **cluster** | Namespaced **Widget** CRD |
| `SecurityContextConstraints` `crane-complex-demo-scc` | `security.openshift.io/v1`, **cluster** | Custom SCC; `users` includes `system:serviceaccount:crane-complex-demo:app-sa` |
| `ServiceAccount` `app-sa` | core/v1, namespace | Workload + RBAC + SCC linkage |
| `ConfigMap` `app-config` | core/v1, namespace | Sample config volume |
| `Widget` `demo-widget` | `stable.example.com/v1`, namespace | Custom resource instance |
| `ClusterRole` `crane-complex-demo-app-role` | `rbac.authorization.k8s.io/v1`, **cluster** | Rules: `pods` + `widgets` read |
| `ClusterRoleBinding` `crane-complex-demo-app-sa-binding` | `rbac.authorization.k8s.io/v1`, **cluster** | Binds `app-sa` → above ClusterRole |
| `Deployment` `complex-demo-app` | `apps/v1`, namespace | UBI minimal `sleep`, mounts ConfigMap, uses `app-sa` |
| `Service` `complex-demo-svc` | core/v1, namespace | ClusterIP fronting the workload |

This is a **small but multi-surface** stack (CRD + CR + SCC + cluster RBAC + standard workload objects), not a full OperatorHub operator.

## Crane behavior reminder (unchanged in this experiment)

- **`--cluster-scoped-rbac`** only **lists and filters** these cluster types: **ClusterRole**, **ClusterRoleBinding**, **SecurityContextConstraints** ([`cmd/export/cluster.go`](../cmd/export/cluster.go)).
- **`CustomResourceDefinition`** is **not** in that allowlist: the **CRD definition** does **not** appear under `resources/<ns>/_cluster/` even when a **Widget** instance is exported.
- Namespaced **CR instances** are exported like any other namespaced API **if** the user can **`list`** that resource in the namespace.

## Experiment A — cluster admin (`kube:admin`) + `-c`

**Commands (illustrative):**

```bash
oc project crane-complex-demo
./crane export --export-dir ./export-complex-kubeadmin-c --cluster-scoped-rbac
```

**Log highlights:**

- ClusterRoleBinding `crane-complex-demo-app-sa-binding` accepted (subject `ServiceAccount` `app-sa`).
- ClusterRoles: **348** listed, **1** kept after filter (`crane-complex-demo-app-role`).
- SCCs: **13** listed, **1** kept (`crane-complex-demo-scc`) — matched via `users` entry for `system:serviceaccount:crane-complex-demo:app-sa`.
- Resource type **`widgets`** added and written (preferred version discovery succeeded).

**`resources/crane-complex-demo/_cluster/` (3 files):**

- `ClusterRoleBinding_rbac.authorization.k8s.io_v1_clusterscoped_crane-complex-demo-app-sa-binding.yaml`
- `ClusterRole_rbac.authorization.k8s.io_v1_clusterscoped_crane-complex-demo-app-role.yaml`
- `SecurityContextConstraints_security.openshift.io_v1_clusterscoped_crane-complex-demo-scc.yaml`

**Namespaced export (includes CR):**

- Example: `Widget_stable.example.com_v1_crane-complex-demo_demo-widget.yaml`

**CRD definition:** not present under `_cluster/` (expected with current Crane).

## Experiment B — namespace admin (`crane-complex-user`) + `-c`

**Commands (illustrative):**

```bash
./crane export \
  --export-dir ./export-complex-nsadmin-c \
  --cluster-scoped-rbac \
  --kubeconfig /tmp/crane-complex-user-kubeconfig
```

(`genericclioptions` also supports `--context` if the non-admin user shares a kubeconfig with other identities.)

**Log highlights:**

- **ClusterRoleBinding** cluster `List` → **403**; message: *no ClusterRoleBinding resources have been collected* (see `failures/`).
- **SecurityContextConstraints** cluster `List` → **403**.
- **ClusterRole** cluster `List` still succeeded for this user (**348** objects); **0** remained after filter (no accepted CRBs).
- **Widget** namespaced `List` → **403** for `crane-complex-user` — OpenShift **`admin`** in a namespace does **not** automatically grant `list` on every **custom API**; `widgets.stable.example.com` needs an explicit `Role`/`RoleBinding` if the namespace admin must export CRs.

**`resources/crane-complex-demo/_cluster/`:** directory exists but is **empty** (no YAML files).

**Namespaced files:** **27** files at the namespace root (Deployment, Pod, Service, ConfigMaps, Secrets, ServiceAccounts, RoleBindings, etc.) — **no** `Widget_*` file.

**Failures:** many `failures/crane-complex-demo/*.yaml` entries, including:

- `clusterrolebindings.yaml` — 403 at cluster scope
- `securitycontextconstraints.yaml` — 403 at cluster scope  
- `widgets.yaml` — 403 for `list` on `widgets` in `crane-complex-demo`

Additional failure files appear for other cluster-scoped API types that discovery enumerates but the user cannot list (same pattern as earlier `crane-rbac-demo` non-admin runs).

## Comparison

| Aspect | Cluster admin + `-c` | Namespace admin + `-c` |
|--------|----------------------|-------------------------|
| Namespaced core objects (Deploy, Pod, Svc, CM, Secret, SA, …) | Exported | Exported |
| **Widget** CR | Exported | **Not** exported (`403` on `list widgets`) |
| **ClusterRoleBinding** / **SCC** cluster `List` | Success | **403** |
| **ClusterRole** cluster `List` | Success | Success (OpenShift often allows read-only enumeration for authenticated users) |
| **`_cluster/`** content | CR + CRB + SCC YAML | **Empty** (filter has no CRBs; CR count filtered to 0) |
| **`failures/`** noise | Lower | High (forbidden cluster + custom API types) |

## Findings

1. **CRD vs CR:** The **Widget** instance is a namespaced object; the **CRD** is cluster-scoped but **not** part of `--cluster-scoped-rbac`. To export CRD YAML with today’s Crane would require a separate feature (see [#186](https://github.com/migtools/crane/issues/186)).
2. **SCC:** A **custom SCC** referenced via `users: system:serviceaccount:<ns>:<sa>` is **picked up** by the SCC filter when cluster `List` succeeds (Experiment A).
3. **Namespace admin:** Without cluster `list` on **ClusterRoleBinding** (and **SCC**), **`_cluster/` is empty** even with `-c`; namespaced export can still **complete**.
4. **Custom resources:** Namespace **`admin`** is **not** the same as “can list every CRD-backed resource in the project.” Grant **`widgets`** (or `*`) via a **Role** if exports must include that API for non-cluster-admins.
5. **Discovery + restricted credentials:** A large **`failures/`** set is expected when discovery walks many APIs the user cannot list at cluster scope; operational UX could be improved with a summary warning (also discussed in [#186](https://github.com/migtools/crane/issues/186)).

## Local output directories (from the run that produced this doc)

- `./export-complex-kubeadmin-c/`
- `./export-complex-nsadmin-c/`

These are **runtime artifacts**; they are not required to stay in the repo. Remove them when finished reviewing.

## Cleanup (cluster admin)

```bash
oc delete widget demo-widget -n crane-complex-demo --ignore-not-found
oc delete project crane-complex-demo
oc delete scc crane-complex-demo-scc
oc delete crd widgets.stable.example.com
# Optional: remove crane-complex-user from htpasswd and delete RoleBinding if re-running tests
```
