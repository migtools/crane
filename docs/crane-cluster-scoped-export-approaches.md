# Approaches for including cluster-scoped resources in `crane export`

This document outlines **possible strategies** for extending Crane’s **`export`** command so that **cluster-scoped** objects relevant to a namespace (or workload) appear in the export, without implying every future idea below is implemented.

**Today:** Crane **`export`** always attempts cluster-scoped RBAC export (see §1).

---

## 1. Current approach: list cluster-wide, then shrink by exported ServiceAccounts

**How it works (high level)**

1. Export **namespaced** objects for the target namespace (and optional label selector), including **ServiceAccounts**.
2. Additionally **`List`** (cluster-wide) only a **fixed allowlist** of cluster types: **ClusterRoleBinding**, **ClusterRole** (`rbac.authorization.k8s.io`), and **SecurityContextConstraints** (`security.openshift.io` on OpenShift).
3. **Filter** those cluster lists so only objects **related to the exported ServiceAccounts** remain:
   - **ClusterRoleBindings** whose subjects include: a matching **ServiceAccount**; or **`Group`** `system:serviceaccounts:<namespace>` when that namespace is among exported SAs; or **`User`** `system:serviceaccount:<namespace>:<saName>` matching an exported SA.
   - **ClusterRoles** referenced by accepted CRBs (`RoleRef` to `ClusterRole`).
   - **SCCs** linked via accepted CRB `roleRef`, `system:openshift:scc:*` **ClusterRole** names, **`users`** entries `system:serviceaccount:<ns>:<sa>`, or **`groups`** containing `system:serviceaccounts:<ns>` for an exported SA namespace.
4. Write cluster-scoped YAML under **`resources/<namespace>/_cluster/`**.

**Pros**

- Conceptually simple: one pass of discovery + list, then in-memory graph trim.
- Aligns with the mental model “take everything cluster-wide for these kinds, then keep only what the namespace’s SAs need.”
- Produces files similar to what operators expect (full object YAML).

**Cons**

- Requires **cluster-scoped `list` (and usually `get`)** on those types. A **namespace admin** context typically **cannot** run this step; cluster lists **fail** (e.g. 403), errors land under **`failures/`**, and **`_cluster/`** stays empty.
- **Scales with cluster size**: full list of CRBs/CRs (and SCCs) even though most rows are discarded.
- **Allowlist is fixed** in code; other cluster types (CRDs, webhooks, storage classes, etc.) are out of scope unless extended.

**When it fits**

- Export is run with credentials that can list cluster RBAC (and SCC on OpenShift), but you still want **narrow** output tied to the namespace’s SAs.

---

## 2. Velero-style / “related items” approach: from namespaced objects, resolve dependencies

**How it would work (conceptual)**

1. Export namespaced resources as today (or a defined subset: workloads, SAs, PVCs, etc.).
2. For each **seed** object (or only for types that matter), run **pluggable resolvers** that return **`{group, resource, namespace?, name}`** tuples for **additional** objects to fetch.
3. For each tuple, **`Get`** (not cluster-wide **`List`**) the object; optionally recurse (with **deduplication** and **depth limits**) as new seeds produce more related IDs.
4. Merge related cluster-scoped objects into the export (e.g. still under **`_cluster/`**).

**Examples of resolvers (illustrative)**

- **ServiceAccount** → CRBs that reference that SA → CRs (or Roles) referenced by those bindings (mirrors Velero’s backup item action idea).
- **PVC** → bound **PersistentVolume** (Velero does this).
- **Pod** → **PodSecurity** / admission-linked resources only if you define explicit rules (not automatic in core Kubernetes).

**Pros**

- **No cluster-wide list** of CRBs/CRs: only **`Get`** on known names, which can be easier to grant via **narrow** RBAC (in theory: `get` on named resources or custom roles—still often hard without broad permissions in practice).
- **Extensible**: new related kinds without changing the core loop.
- Behavior can be documented to **match Velero/OADP** semantics for parity with MTC-style migrations.

**Cons**

- **Implementation complexity**: ordering, cycles, dedup, partial failures, and which seeds to use (every object vs SAs only).
- **RBAC is not automatically “namespace-admin friendly”**: `get` on a **ClusterRoleBinding** by name still requires knowing the name first (from subject index or from annotations)—you still need some way to discover bindings. In practice you often either **list** CRBs (back to §1).
- **Incomplete graphs** unless resolvers are comprehensive (aggregation, indirect references, OpenShift-only edges).

---

## MTC / Velero: cluster-scoped kinds (reference list)

For **Migration Toolkit for Containers** (mig-controller + Velero), a typical **namespace migration** uses `spec.includeClusterResources: nil` on the Velero `Backup`. Cluster-scoped objects then enter the archive mainly via **related-item logic** (backup item actions), **stage backup resource lists**, and the **CRD post-pass**—not via “list every cluster-scoped type.”

**Copy-paste list — usually included (core path):**

```
rbac.authorization.k8s.io/ClusterRole
rbac.authorization.k8s.io/ClusterRoleBinding
/v1/PersistentVolume
/v1/Namespace
apiextensions.k8s.io/CustomResourceDefinition
```

(`CustomResourceDefinition` only when instances of that API are in the backup and Velero’s CRD logic applies.)

**Copy-paste list — often conditional (CSI / snapshotting / Velero version):**

```
snapshot.storage.k8s.io/VolumeSnapshot
snapshot.storage.k8s.io/VolumeSnapshotContent
snapshot.storage.k8s.io/VolumeSnapshotClass
```

**Copy-paste list — not guaranteed by core MTC + Velero (plugins or broader backup only):**

```
security.openshift.io/SecurityContextConstraints
```

Other cluster-scoped kinds (e.g. `StorageClass`, admission webhooks, `CSIDriver`) are **out of scope** for the default story unless **plugins**, **MigPlan `includedResources`**, or non-default Velero behavior adds them.

See also `docs/mtc-cluster-level-resources.md` for how this ties to item actions and `includeClusterResources`.

