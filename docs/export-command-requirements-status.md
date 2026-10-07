# Export command — requirements status

This document maps product expectations for **`crane export`** to the current implementation in this repository (as of the analysis date). Status values are:

| Status | Meaning |
|--------|---------|
| **fulfilled** | Matches the requirement in normal use |
| **implemented** | Substantially present; gaps or caveats in notes |
| **pending** | Not implemented; valid requirement |
| **inprogress** | Partially implemented (only use if work is visibly underway) |
| **acked** | Recognized product intent; not necessarily shipped |
| **dispute** | Requirement is ambiguous, environment-dependent, or not universally satisfiable |

---

## Authentication, kubeconfig, and authorization

### Reuse kubeconfig and kubectl-style login

| Status | **fulfilled** |
|--------|---------------|

**Note:** Export uses `k8s.io/cli-runtime/pkg/genericclioptions.ConfigFlags` (same building blocks as `kubectl`). Users authenticate with their existing kubeconfig (and tools like `kubectl login` / `oc login` where applicable). No separate Crane login flow.

---

### Reuse kubectl assumptions; API server for authorization

| Status | **fulfilled** |
|--------|---------------|

**Note:** Credentials and TLS come from the kubeconfig / REST config. **Authorization** is entirely enforced by the Kubernetes API server (RBAC, admission, etc.). Crane lists and gets resources like any other client; forbidden lists appear under `failures/` with errors logged.

---

### Reuse kubeconfig contexts across commands

| Status | **fulfilled** |
|--------|---------------|

**Note:** Standard flags apply: `--kubeconfig`, `--context`, `--user`, `--cluster`, `--as`, `--as-group`, etc., via `ConfigFlags`. The active context determines defaults unless overridden.

---

## Namespace, filtering, and visibility

### Namespace target; default to current namespace from kubeconfig

| Status | **fulfilled** |
|--------|---------------|

**Note:** Namespace is resolved with `ToRawKubeConfigLoader().Namespace()` (same as kubectl). Use `-n` / `--namespace` to override. Explicit empty `--namespace` is rejected to avoid silent wrong-namespace exports.

---

### Optional filters (e.g. labels); specify version for target resources

| Status | **implemented** |
|--------|-----------------|

**Note:**

- **Labels:** `--label-selector` / `-l` is supported and validated with `labels.Parse`.
- **Target API version:** There is **no** flag to export a specific API version (e.g. `apps/v1beta1` vs `apps/v1`). Discovery uses **server-preferred** resources only (`ServerPreferredResources()`), so each resource kind is exported at the server’s preferred version for that kind.

---

### Export all versions of a resource when a flag is set

| Status | **pending** |
|--------|-------------|

**Note:** Not implemented. Code uses preferred versions only. A `TODO` in `resourceToExtract` references putting some behavior behind a flag (currently related to skipping `Event`). Fulfilling “all versions” would require iterating non-preferred `APIResourceList` entries (or equivalent) and listing each `(group, version, resource)` the user cares about, with clear semantics for duplicate logical objects.

---

### User can see all necessary resources

| Status | **dispute** |
|--------|-------------|

**Note:** “Necessary” depends on the migration. The tool exports what discovery admits, listing permits, and filters allow (e.g. cluster-scoped resources are limited to related RBAC and OpenShift SCC; `Event` is skipped). Resources the user cannot list, or types filtered out, are not exported—some failures are recorded under `export/failures/<namespace>/`. Documenting “coverage” expectations (and gaps) is more accurate than promising “all necessary” in every cluster.

---

### User knows the namespace of exported resources

| Status | **fulfilled** |
|--------|---------------|

**Note:** Layout is `resources/<namespace>/` for namespaced objects; cluster-scoped companions go under `resources/<namespace>/_cluster/`. Filenames include the namespace segment (or `clusterscoped` for cluster-scoped objects). Each manifest includes `metadata.namespace` where applicable.

---

### Personas: namespace admin vs application admin

| Status | **implemented** |
|--------|-----------------|

**Note:**

- **Namespace admin (whole namespace):** Supported by exporting the namespace directory contents (plus related cluster RBAC/SCC and CRDs as implemented).
- **Application admin (subset):** Partially supported via **label selector** only. There is no built-in **field selector**, allow/deny resource list, or name glob—so “specific set of resources” beyond labels may require post-export filtering or future CLI features.

---

### Flexibility in defining export criteria

| Status | **implemented** |
|--------|-----------------|

**Note:** Today: kubeconfig-driven identity/context, namespace, label selector, `export-dir`, QPS/burst tuning, CRD group skip/include, impersonation extras. Not flexible to the degree of arbitrary CEL/query or full `kubectl get`-style resource expressions.

---

## Output format and layout

### Support writing YAML and/or JSON

| Status | **implemented** |
|--------|-----------------|

**Note:** Manifests and failure records are written with `sigs.k8s.io/yaml` (YAML). **JSON output is not implemented** as a user-selectable format. Users can convert offline if needed.

---

### One file per resource version; same logical resource at multiple API versions → multiple files

| Status | **pending** |
|--------|-------------|

**Note:** Filenames encode **Kind_group_version_namespace_name** (see `getFilePath`), which disambiguates versions **if** multiple versions were exported. Because only the **preferred** version per kind is listed today, the “multiple versions of the same object” case does not occur in practice. Implementing multi-version export is prerequisite.

---

### Each namespace maps to one directory

| Status | **fulfilled** |
|--------|---------------|

**Note:** Namespaced files live under `resources/<namespace>/`. Cluster-scoped exports for that run use `resources/<namespace>/_cluster/` (still one export run anchored to one namespace).

---

### Export covers all needed definitions

| Status | **dispute** |
|--------|-------------|

**Note:** The pipeline aims for broad, migration-relevant coverage (discovered types with required verbs, related CRDs, filtered cluster RBAC/SCC). It does **not** guarantee every possible dependency (e.g. cluster-scoped resources outside the allowlist, cross-namespace references, or data plane artifacts). Treat as **best-effort snapshot** validated for your environment and policies.

---

## Summary table

| Topic | Status |
|-------|--------|
| Kubeconfig / context / kubectl-compatible flags | fulfilled |
| API-server authorization | fulfilled |
| Namespace default & override | fulfilled |
| Label-based filtering | implemented |
| Choose target API version / export all versions | pending |
| YAML output | implemented (only format) |
| JSON output | pending |
| One directory per namespace | fulfilled |
| Namespace visible in paths and manifests | fulfilled |
| Multi-version files for same object | pending (blocked by single-version listing) |
| “All necessary” / “all definitions” coverage | dispute |
| Personas (full NS vs app subset) | implemented (subset via labels only) |
