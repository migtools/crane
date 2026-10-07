# MTC and cluster-level resources linked to a migrated namespace

This document describes how **Migration Toolkit for Containers (MTC)** — via **mig-controller** and **Velero** — discovers, includes, and reapplies **cluster-scoped** (and cluster-relevant) resources for an application migration. 

**Scope:** Behavior as implemented in **mig-controller** (Konveyor fork) with **Velero v1.7.x** as the **declared Go dependency** (see `go.mod` on the controller repo). 

**References (upstream):**

- [mig-controller](https://github.com/migtools/mig-controller) — `pkg/controller/migmigration/backup.go`, `restore.go`
- [Velero v1.7.1](https://github.com/vmware-tanzu/velero/tree/v1.7.1/pkg/backup) — classic layout: `item_collector.go`, `item_backupper.go`, `service_account_action.go`, `backup_pv_action.go`
- [Velero v1.18.0](https://github.com/vmware-tanzu/velero/tree/v1.18.0/pkg/backup) — `item_collector.go`, `item_backupper.go`, `pkg/backup/actions/` (built-in actions moved here)

---

## 1. High-level model


| Phase         | Component                        | Role                                                                                       |
| ------------- | -------------------------------- | ------------------------------------------------------------------------------------------ |
| Orchestration | **mig-controller**               | Creates **Velero `Backup`** / `Restore` CRs, namespaces, storage hooks, stage labels, etc. |
| Capture       | **Velero** (source cluster)      | Enumerates resources, writes backup payload (object store), runs **backup item actions**   |
| Apply         | **Velero** (destination cluster) | Restores objects from backup; **Velero’s service account** performs API creates            |


**Important:** The interactive user (or namespace-scoped subject) is **not** the principal that creates cluster-scoped objects on the target. **Velero’s installation** is expected to use credentials with sufficient scope (often **cluster-admin** in documentation and practice). That is how MTC sidesteps the “namespace admin cannot create ClusterRoleBinding” problem operationally.

---

## 2. What mig-controller sets on the Velero `Backup`

In `buildBackup` (`pkg/controller/migmigration/backup.go`):

- `**spec.includedNamespaces`:** Namespaces from the **MigPlan** (the application namespaces being migrated).
- `**spec.includeClusterResources`:** Always `**nil`** in code (`var includeClusterResources *bool = nil`).

With `**nil**`, Velero treats cluster inclusion as **“auto.”** Combined with **only some namespaces** (not a full-cluster backup), this **suppresses bulk backup of most cluster-scoped resource types** on the main collection path; related cluster objects still arrive via **item actions** (§3.3).

Additional fields on the **initial** backup:

- `**includedResources` / `excludedResources`:** From controller **settings** plus **MigPlan** status (defaults exclude e.g. certain volume/image types from the initial pass in favor of stage/direct flows).
- `**labelSelector`:** Optional, from **MigPlan** `spec.labelSelector`.
- `**spec.includedResources` from MigPlan:** Optional **GroupKind** list resolved to API resource names via discovery — appended to the Velero backup. **Note:** **cluster-scoped bulk collection** is still gated by the collector / filter logic (§3.2); this does not automatically imply “list every cluster-scoped instance.”

**Stage backup** (second pass) uses a **fixed set** of resource kinds (e.g. pods, PVCs, secrets, configmaps, serviceaccounts, namespaces, sometimes imagestreams) and a **label selector** so only objects labeled for that migration are included.

---

## 3. How Velero decides what to back up (cluster-scoped vs namespaced)

### 3.1 Discovery helper (types, not objects)

Velero uses a **discovery helper** (`pkg/discovery`) to walk **API groups** and **resource types** (same general mechanism as `kubectl` / client-go discovery). This answers **what kinds exist** and **preferred versions**, not “list every object.”

### 3.2 Main collection path — no bulk list of arbitrary cluster-scoped types

**Velero 1.7.x:** In `pkg/backup/item_collector.go`, `getResourceItems` returns early for cluster-scoped types (other than `namespaces`) when `includeClusterResources == nil` and the backup is **not** for all namespaces — so there is **no** cluster-wide `List` of e.g. every `clusterrolebinding` in that phase.

**Velero 1.18.x (legacy filters):** The same rule is applied in `GlobalIncludesExcludes.ShouldInclude` (`pkg/util/collections/includes_excludes.go`): cluster-scoped types are skipped (except `namespaces`, which can be filtered later) when `includeClusterResources == nil` and namespaces are not `IncludeEverything()`. The **item collector** was refactored (e.g. `resourceIDsMap` for explicit additional items) but **MTC-style backups still hit this legacy-filter path** when `includedResources` / `excludedResources` are set (as MTC does).

So for a **typical MTC migration** (subset of namespaces + `includeClusterResources: nil`):

- **There is no “fetch all cluster-scoped resources then filter”** on the main path for arbitrary cluster types.

### 3.3 Item backupper and backup item actions — how *related* cluster objects are backed up

Section §3.2 explained that Velero usually **does not** bulk-list every cluster-scoped type for a namespace-only backup. **This section** is how **specific** cluster-scoped objects (and other dependencies) still get into the archive: **backup item actions**, invoked **per object** while the backup runs.

#### When actions run (not “one hook for the whole backup”)

Velero registers a set of **BackupItemAction** implementations (**built-in** in the Velero binary **plus** **plugins**, e.g. OpenShift). During `**backupItem`** (`item_backupper`), for **each individual Kubernetes object** that has already been selected for backup (each `ServiceAccount`, each `PVC`, each `Pod`, …), Velero:

1. Runs **pre-hooks** (if configured).
2. Iterates **resolved actions** whose `**AppliesTo()`** matches that object’s **resource type** (and passes namespace / label checks).
3. For each matching action, calls `**Execute(item, backup)`**.

So actions are **per-resource-instance**, **during** backup of that instance — not a separate global pass over the cluster.

#### What `Execute` returns

`Execute` returns:

- The (possibly **updated**) object to continue backing up, and  
- `**[]ResourceIdentifier`** — **additional** objects Velero should include (each identifier is API **group/resource** + **name** + optional **namespace**).

Velero then **resolves** the type via the discovery helper, **GETs** each additional object from the API, and calls `**backupItem` again** on it. That **recursive** `backupItem` run applies namespace/resource filters and can run **further actions** on the new object. This is how a cluster-scoped `ClusterRoleBinding` enters the backup without ever appearing in the §3.2 bulk list for that type.

#### Built-in vs custom actions

- **Built-in:** Shipped with Velero (ServiceAccount, PVC, Pod-related behavior, etc.).
- **Custom:** Provided by **plugins** in the Velero image (Konveyor/OpenShift plugins extend this set).

#### Post-pass CRD backup (separate mechanism)

After item processing, Velero may also add **CustomResourceDefinition** objects for API types where at least one **custom resource instance** was backed up, when `includeClusterResources == nil` — see `backup.go` CRD logic. That is **not** implemented as a BackupItemAction; it is a **finalization** step tied to “we backed up CRs of this type.” **OpenShift `SecurityContextConstraints` are not CRDs** — they do not appear through this CRD post-pass.

#### What item actions do *not* cover (example: custom SCCs)

**Upstream Velero** has **no** built-in **BackupItemAction** for `**security.openshift.io` `SecurityContextConstraints`**. The usual **ServiceAccount → ClusterRoleBinding → ClusterRole** chain (§3.3) does **not** walk SCC objects: SCCs are separate cluster-scoped definitions that workloads *use* via service account / namespace annotations / admission, not RBAC bindings Velero’s SA action enumerates.

For a **typical MTC backup** (`includeClusterResources: nil`, namespace subset), **custom SCC resources are therefore not included by default** as “related” objects in the same sense as CRBs. They are **not** bulk-listed on the main collector path either (§3.2). **Konveyor OpenShift Velero plugins** may add other behaviors (restore-time tweaks, actions for specific types); treat **SCC migration** as **cluster operational data** you plan explicitly — e.g. recreate SCCs on the target, GitOps, a full-cluster or explicit-resource backup slice, or plugin-specific support — rather than something MTC/Velero guarantees out of the box.

#### Core examples (same pattern, different paths by version)


| Trigger object     | What gets added                                                                         | Code (reference)                                                                                                                                     |
| ------------------ | --------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| **ServiceAccount** | **ClusterRoleBinding** + referenced **ClusterRole** for bindings that reference that SA | v1.7: `pkg/backup/service_account_action.go` — v1.18: `pkg/backup/actions/service_account_action.go` (`actionhelpers.RelatedItemsForServiceAccount`) |
| **PVC** (bound)    | **PersistentVolume**                                                                    | v1.7: `pkg/backup/backup_pv_action.go` — v1.18: PVC action under `pkg/backup/`                                                                       |


#### Takeaway

**RBAC at cluster scope** for migrated workloads is primarily: **for each ServiceAccount that is backed up, also GET and back up the CRBs/CRs that reference it** — not “export all ClusterRoleBindings on the cluster.

---

## 4. How this maps to “linked” cluster resources (SCC, aggregation, custom cluster RBAC)


| Concern                                                | MTC / Velero behavior (typical)                                                                                                                                                                                                                                                                                                                                 |
| ------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **ClusterRoleBinding → ServiceAccount in migrated NS** | **Yes** — core **ServiceAccount** backup item action pulls **CRB + ClusterRole** when the binding references that SA.                                                                                                                                                                                                                                           |
| **Custom ClusterRoles (aggregated, etc.)**             | Included **if** referenced by a **ClusterRoleBinding** that passes the action (by **ClusterRole** name). Deep aggregation graphs are **not** exhaustively expanded beyond that linkage unless plugins or other logic add more.                                                                                                                                  |
| **Custom SCCs (OpenShift)**                            | **Not included by default** on the standard related-items path: **no** core Velero item action for `**SecurityContextConstraints`**; they are **not** part of the SA→CRB→CR graph (§3.3). **mig-controller** does not define a separate “export all SCCs” step. Plan **explicit** SCC handling on the target (or broader backup scope / plugins if applicable). |
| **Arbitrary cluster-scoped CRDs / cluster resources**  | Generally **not** bulk-discovered for namespace-only backup; may appear via **CRD** follow-up for **CR instances** actually backed up, or **plugins**, or if `**includeClusterResources`** were **true** / full-cluster backup (not MTC’s default).                                                                                                             |


---

## 5. Summary

- **mig-controller** configures **Velero** with **namespace-scoped** backups and `**includeClusterResources: nil`**.
- **Velero’s item collector** skips **bulk** enumeration of most **cluster-scoped** types for that combination.
- **Cluster-scoped resources** tied to the app are still migrated primarily through **backup item actions** (notably **ServiceAccount → ClusterRoleBinding + ClusterRole**, **PVC → PV**) and **plugin** behavior, plus **CRD** backup when CRs are included. **Custom SCCs** are **out of scope** for that default graph (§3.3).
- **Target cluster application** of those resources is performed by **Velero**, with permissions granted at **install time** — addressing the namespace-admin limitation by **separating human role from migration runtime identity**.

