# Cluster workloads (cluster-admin) — requirements status

This document maps expectations for **migrating cluster workloads** when the principal can run as **cluster-admin** (full list/get on cluster-scoped objects, single-phase apply over rendered output). It aligns with the current **Crane CLI** behavior in this repository (export → transform → apply; `kubectl` for cluster apply in typical workflows).

Status values:

| Status | Meaning |
|--------|---------|
| **fulfilled** | Matches the requirement in normal use (with any noted caveats) |
| **implemented** | Present in code or workflow; gaps or caveats in notes |
| **pending** | Not implemented; requirement remains valid |
| **inprogress** | Partially implemented (only if work is visibly underway in tree) |
| **acked** | Recognized product intent; not necessarily shipped as automation |
| **dispute** | Ambiguous, environment-dependent, or not universally satisfiable by a CLI |

---

## 1. Handle permission-related issues of a workload

| Status | **implemented** |

**Note:** **Valid requirement.** Export surfaces authorization problems instead of silently omitting them: list/get failures are recorded under `export/failures/<namespace>/`, and common cases (for example `Forbidden`) are logged with context (see `resourceToExtract` in `cmd/export/discover.go`). The tool does **not** automatically repair RBAC, impersonate broader rights, or replay with escalated credentials. **How to fulfill further:** keep failure records actionable (resource type, GVK, error); optional future steps include a dedicated validate/dry-run against a declared kubeconfig and clearer aggregation of “permission blockers” for admins.

---

## 2. Handle ServiceAccount migrations that rely on a ClusterRoleBinding

| Status | **fulfilled** |

**Note:** **Valid requirement.** Export includes related **ClusterRoleBinding**, **ClusterRole**, and (on OpenShift) **SecurityContextConstraints** when they can be tied to **ServiceAccounts present in the exported namespace**—subjects matched include ServiceAccount, `User` principals of the form `system:serviceaccount:…`, and `Group` subjects `system:serviceaccounts:<namespace>` (see `cmd/export/cluster.go`). **Caveats:** matching is **heuristic** (only exported SAs in scope; label-scoped exports narrow SAs); bindings that reference only broad groups (for example `system:authenticated`) are intentionally not pulled in.

---

## 3. Handle when a Pod is using a custom SCC

| Status | **implemented** |

**Note:** **Valid requirement for OpenShift.** SCC objects are exported when they pass the filter in `acceptSecurityContextConstraints`: linkage via accepted **ClusterRoleBinding** (including `RoleRef` to `ClusterRole` named `system:openshift:scc:<name>`), direct SCC `RoleRef` on a binding, or **users** / **groups** on the SCC that reference exported service accounts or `system:serviceaccounts:<ns>` (see `cmd/export/cluster.go`). **Gaps:** SCC assignment is ultimately admission-driven; if the only linkage is indirect or outside these paths, export may not include the SCC. **How to fulfill further:** document OpenShift-specific edges; optional enhancement to consider pod annotations or additional RBAC shapes if product scope expands.

---

## 4. Handle Role aggregation

| Status | **dispute** |

**Note:** **Partially valid as stated.** Export emits **ClusterRole** objects as stored in etcd (including `aggregationRule` when present). Kubernetes **aggregates** rules at runtime from other `ClusterRoles` matching the selector; those effective rules are **not** materialized into the exported YAML. For migration, treating the exported object as the **source of truth for the ClusterRole resource** is correct; expecting a **fully expanded** rule set in one manifest is not how the API stores aggregated roles. **How to fulfill if “expanded rules” are required:** export related aggregated `ClusterRoles` explicitly or use cluster-specific documentation/tools—not automatic expansion in Crane today.

---

## 5. Assume best effort — heuristics are not exhaustive

| Status | **acked** |

**Note:** **Valid product stance.** The export path deliberately uses discovery, filters, and related-object heuristics (RBAC/SCC filtering, CRD collection for referenced API groups, skipped types such as `Event`). It does not guarantee every cross-namespace or cluster-wide dependency. **How to fulfill:** treat output as a **snapshot** to validate in CI and on target; extend heuristics only with clear, test-scoped rules to avoid pulling unrelated cluster objects.

---

## 6. Some admins guard cluster-scoped changes and want close review

| Status | **acked** |

**Note:** **Valid operational concern.** Crane does not enforce policy gates or approval workflows. **What exists today:** cluster-scoped manifests are isolated under `resources/<namespace>/_cluster/` (and called out in logs when present), which supports **human review** and **split apply** (namespace vs cluster phases) in documented workflows. **How to fulfill further:** optional review summaries, policy hooks, or integrate with GitOps review—outside current CLI scope.

---

## 7. Admins need visibility into cluster-scoped dependencies (where to focus)

| Status | **implemented** |

**Note:** **Valid requirement.** Reviewers can focus on **`_cluster/`** and log lines that summarize exported cluster-scoped kinds (see `ClusterScopeHandler` logging in `cmd/export/cluster.go`). There is **no** separate machine-readable “dependency report” (graph, risk score, or cross-reference to Pods) in-repo. **How to fulfill further:** emit a small manifest or report (for example `cluster-dependencies.yaml` or Markdown) listing cluster-scoped files and why they were included.

---

## 8. User option: tool imports cluster-level resources vs user imports them

| Status | **pending** |

**Note:** **Valid requirement.** Operationally, users can **apply only** `output/resources/<namespace>/` vs **also** `output/resources/_cluster/` (or apply `_cluster` with a different kubeconfig/context). Crane does **not** expose a first-class flag that means “run kubectl only for namespaced resources” or “skip cluster phase”—that remains a **documented kubectl workflow** (see internal e2e planning under `.cursor/plans/cluster_workload_e2e.plan.md`). **How to fulfill:** add an optional subcommand or flags that wrap `kubectl apply` with explicit include/exclude of cluster objects, or publish a small companion script; product decision.

---

## 9. Default cluster-scoped resources on the target are not rewritten automatically

| Status | **fulfilled** |

**Note:** **Valid requirement for “no silent cluster mutation.”** Transform is **plugin-driven** JSON Patch (`crane transform`); there is no built-in step that rewrites arbitrary cluster-scoped defaults on the target. Cluster-scoped YAML is carried forward unless a plugin patches it. **Caveat:** users remain responsible for **conflicts** with pre-existing cluster objects when they choose to apply `_cluster/`.

---

## 10. CRs are exported; CRDs are often assumed to exist on the target

| Status | **implemented** |

**Note:** **Valid requirement; wording is slightly loose.** Custom resource **instances** export like other discovered types. **CRD** objects for non-built-in API groups can be **exported into `_cluster/`** via `collectRelatedCRDs` (`cmd/export/crd.go`), subject to `--crd-skip-group` / `--crd-include-group`. Many migrations **assume** CRDs are installed by operators on the target to avoid CRD ownership conflicts—both patterns are supported by controlling CRD export. **How to fulfill:** document the flags and the “operator-owned CRD” vs “export CRD” choice per environment.

---

## 11. If we have target cluster access, use its GVK data to drive export alignment; otherwise preferred versions

| Status | **pending** |

**Note:** **Valid as a product direction; not implemented in `crane export`.** Export uses **only the source** discovery client and **`ServerPreferredResources()`** (see `discoverPreferredResources` in `cmd/export/discover.go`). There is **no** second kubeconfig for “target alignment” in the CLI. **How to fulfill:** dual discovery (source + target), compatibility matrix, and optional export/version remapping—or integrate with external validation (compare to MTC-style discovery comparison described in `docs/mtc-resource-versioning-and-crane-options.md`).

---

## 12. Export all supported API versions of each resource

| Status | **pending** |

**Note:** **Valid requirement; not implemented.** Same conclusion as `docs/export-command-requirements-status.md`: only **preferred** versions per logical resource are listed. **How to fulfill:** iterate non-preferred `APIResourceList` entries (or explicit version list flag), define deduplication semantics, and test storage version interactions.

---

## 13. Choose compatible resource versions for import (when they exist on the target)

| Status | **pending** |

**Note:** **Valid requirement; not implemented as automated selection.** Today the exported manifest carries the **source** preferred `apiVersion`. Moving to a **compatible** target version would require conversion rules or server-side defaulting at apply time, plus target discovery—none of which Crane encodes as a single command. **How to fulfill:** pairing with `kubectl convert` (where available), server-side apply, or explicit transform plugins per GVK skew.

---

## Summary table

| Topic | Status |
|-------|--------|
| Permission issues surfaced (not auto-fixed) | implemented |
| ServiceAccount + ClusterRoleBinding path | fulfilled |
| Custom SCC (OpenShift) | implemented |
| Role aggregation (effective vs stored rules) | dispute |
| Best-effort / non-exhaustive heuristics | acked |
| Admins want guarded review of cluster changes | acked |
| Visibility into cluster-scoped dependencies | implemented (layout + logs; no rich report) |
| Opt-in/opt-out for importing cluster resources in-tool | pending |
| No automatic rewrite of target cluster defaults | fulfilled |
| CRs vs CRDs on target | implemented (flags + assumptions) |
| Target GVK drives export when accessible | pending |
| Export all API versions | pending |
| Pick compatible versions for import | pending |

---

## References in this repo

- Export behavior and version limits: `docs/export-command-requirements-status.md`
- Cluster RBAC/SCC filtering: `cmd/export/cluster.go`, `cmd/export/export.go`, `cmd/export/discover.go`
- CRD export: `cmd/export/crd.go`
- MTC / versioning discussion: `docs/mtc-resource-versioning-and-crane-options.md`
