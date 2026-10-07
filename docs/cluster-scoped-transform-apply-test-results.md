# Cluster-Scoped Resources: Transform & Apply Test Results

**Date:** 2026-04-10
**Issue:** [migtools/crane#189](https://github.com/migtools/crane/issues/189)
**Crane version:** v0.0.6 | crane-lib: v0.0.10
**Cluster:** minikube (Kubernetes, no OpenShift/SCCs)
**Binary:** built from `main` branch (commit aa106d6)

---

## Test Fixture

**Namespace:** `crane-cluster-test`

Resources created:

- 2 ServiceAccounts (`app-sa`, `monitoring-sa`)
- 1 ConfigMap (`app-config`)
- 1 Service (`app-service`, ClusterIP)
- 1 Deployment (`myapp`, 2 replicas, uses `app-sa`)
- 2 ClusterRoles (`crane-test-app-reader`, `crane-test-app-admin`)
- 3 ClusterRoleBindings:
  - `crane-test-app-reader-binding` — subjects: `app-sa` in `crane-cluster-test`
  - `crane-test-app-admin-binding` — subjects: `app-sa` + `monitoring-sa` in `crane-cluster-test`
  - `crane-test-cross-ns-binding` — subjects: `app-sa` in `crane-cluster-test` + `cross-ns-sa` in `crane-cluster-test-2`

**Second namespace:** `crane-cluster-test-2` (1 ServiceAccount: `cross-ns-sa`)

---

## T1: Export + Single-Stage Transform + Apply (Basic)

**Goal:** Verify cluster-scoped resources (ClusterRole, ClusterRoleBinding) flow through the entire pipeline.

**Commands:**

```bash
crane export -n crane-cluster-test
crane transform
crane apply
```

**Results:**


| Check                                                              | Result | Detail                                                                                       |
| ------------------------------------------------------------------ | ------ | -------------------------------------------------------------------------------------------- |
| Export puts CRs/CRBs in `_cluster/`                                | PASS   | 2 ClusterRoles + 3 ClusterRoleBindings in `export/resources/crane-cluster-test/_cluster/`    |
| Transform generates patches for cluster-scoped                     | PASS   | 5 cluster-scoped patch files generated (2 CR + 3 CRB)                                        |
| Patch filenames omit namespace for cluster-scoped                  | PASS   | Format: `rbac.authorization.k8s.io-v1--ClusterRole--<name>.patch.yaml` (no namespace prefix) |
| Kustomization.yaml targets have no namespace for cluster-scoped    | PASS   | `namespace:` field absent in ClusterRole/CRB targets                                         |
| Kustomization.yaml targets have namespace for namespaced resources | PASS   | `namespace: crane-cluster-test` present in Deployment/Service targets                        |
| Metadata stripped (uid, resourceVersion, etc.)                     | PASS   | All 4 fields removed from both namespaced and cluster-scoped                                 |
| Whiteouts applied (Endpoints, Pods, ReplicaSets)                   | PASS   | Only 6 resource type files (no endpoints/pods/replicasets)                                   |
| Apply outputs cluster-scoped to `_cluster/`                        | PASS   | `output/resources/_cluster/ClusterRole_*.yaml` and `ClusterRoleBinding_*.yaml`               |
| Apply outputs namespaced to namespace dir                          | PASS   | `output/resources/crane-cluster-test/*.yaml`                                                 |
| CRB subject namespaces preserved                                   | PASS   | `crane-cluster-test` and `crane-cluster-test-2` intact                                       |


**Observation (not a bug):** `kubectl.kubernetes.io/last-applied-configuration` annotation is NOT stripped. It embeds source-cluster JSON including hardcoded namespace references. This applies to ALL resources (not cluster-specific) and is existing behavior.

**Status: PASS**

---

## T2: Multi-Stage Transform with Cluster-Scoped Resources

**Goal:** Verify cluster-scoped resources survive inter-stage handoff via `kubectl kustomize`.

### T2a: Multi-stage with MISMATCHED stage names

**Commands:**

```bash
this c
mkdir transform/20_StageTwo
crane transform --from-stage "10_StageOne" --to-stage "20_StageTwo" --force
```

**Results:**


| Check                          | Result   | Detail                                                             |
| ------------------------------ | -------- | ------------------------------------------------------------------ |
| Multi-stage runs without error | PASS     | "Successfully completed 2 stage(s)"                                |
| Stage 1 has patches            | **FAIL** | 0 patches generated. Patches directory is empty.                   |
| Stage 1 whiteouts applied      | **FAIL** | Endpoints, Pods, ReplicaSets all present in output                 |
| Stage 1 metadata stripped      | **FAIL** | uid, resourceVersion, creationTimestamp, managedFields all present |
| Stage 2 has patches            | **FAIL** | 0 patches generated                                                |


**BUG-1: Multi-stage silently produces raw (untransformed) output when stage directory name doesn't match a plugin name.**

The multi-stage runner parses the directory name `10_StageOne` and extracts `StageOne` as the plugin name. Since no plugin named `StageOne` exists, zero plugins run. Resources pass through completely raw — no metadata stripping, no whiteouts, no patches. **No warning or error is logged.** This is a silent data integrity issue.

**Root cause:** The directory pattern `<number>_<pluginName>` conflates stage naming with plugin selection. When using `--stage-name` in single-stage mode, the name is purely cosmetic, but multi-stage mode treats it as a plugin selector.

### T2b: Multi-stage with CORRECT plugin names

**Commands:**

```bash
crane transform --stage-name "10_KubernetesPlugin"
mkdir transform/20_KubernetesPlugin
crane transform --from-stage "10_KubernetesPlugin" --to-stage "20_KubernetesPlugin" --force
crane apply
```

**Results:**


| Check                                               | Result | Detail                                    |
| --------------------------------------------------- | ------ | ----------------------------------------- |
| Stage 1: patches generated                          | PASS   | 10 patches                                |
| Stage 1: whiteouts applied                          | PASS   | No Endpoints/Pods/ReplicaSets             |
| Stage 2: reads stage 1 output via kubectl kustomize | PASS   | Clean input (metadata already stripped)   |
| Stage 2: 0 patches (nothing left to strip)          | PASS   | Correct — idempotent                      |
| Stage 2: cluster-scoped resources survive handoff   | PASS   | 2 ClusterRoles + 3 CRBs in stage 2        |
| Stage 2: CRB subjects preserved                     | PASS   | Both namespaces intact                    |
| Apply on final stage                                | PASS   | Correct output with `_cluster/` directory |


**Status: PASS (with correct naming) / FAIL (with mismatched naming — BUG-1)**

---

## T3: Mixed Resources — Patch Correctness

**Goal:** Verify patches are correct for both namespaced and cluster-scoped resources without cross-contamination.

**Analysis from T1 output:**


| Resource                            | Patches Applied                                                                               | Correct? |
| ----------------------------------- | --------------------------------------------------------------------------------------------- | -------- |
| Service (namespaced)                | remove: status, clusterIP, clusterIPs, uid, resourceVersion, creationTimestamp, managedFields | PASS     |
| Deployment (namespaced)             | remove: uid, resourceVersion, creationTimestamp, generation, managedFields, status            | PASS     |
| ClusterRole (cluster-scoped)        | remove: uid, resourceVersion, creationTimestamp, managedFields                                | PASS     |
| ClusterRoleBinding (cluster-scoped) | remove: uid, resourceVersion, creationTimestamp, managedFields                                | PASS     |


**Kustomization target validation:**

- Namespaced targets include `namespace: crane-cluster-test` — PASS
- Cluster-scoped targets omit `namespace:` field — PASS
- No cross-contamination between target types — PASS

**Status: PASS**

---

## T4: Multiple CRBs Referencing Same ClusterRole — Filename Uniqueness

**Goal:** Verify patch filenames don't collide when multiple CRBs reference the same ClusterRole.

**Setup:** `crane-test-app-reader-binding` and `crane-test-cross-ns-binding` both reference `crane-test-app-reader`.

**Results:**


| Check                       | Result | Detail                                                                        |
| --------------------------- | ------ | ----------------------------------------------------------------------------- |
| Unique patch filenames      | PASS   | Filenames use CRB name, not referenced CR name                                |
| All 3 CRBs in resource file | PASS   | `clusterrolebinding.rbac.authorization.k8s.io.yaml` contains 3 YAML documents |
| All 2 CRs in resource file  | PASS   | `clusterrole.rbac.authorization.k8s.io.yaml` contains 2 YAML documents        |


**Patch filenames generated:**

```
rbac.authorization.k8s.io-v1--ClusterRoleBinding--crane-test-app-admin-binding.patch.yaml
rbac.authorization.k8s.io-v1--ClusterRoleBinding--crane-test-app-reader-binding.patch.yaml
rbac.authorization.k8s.io-v1--ClusterRoleBinding--crane-test-cross-ns-binding.patch.yaml
```

**Status: PASS**

---

## T5: Optional Flags (add-annotations) on Cluster-Scoped Resources

**Goal:** Verify `--optional-flags` work on cluster-scoped resources the same as namespaced.

**Command:**

```bash
crane transform --optional-flags '{"add-annotations": "migrated-by=crane,migration-date=2026-04-10"}'
crane apply
```

**Results:**


| Check                                          | Result | Detail                                                           |
| ---------------------------------------------- | ------ | ---------------------------------------------------------------- |
| ClusterRole patches include annotation add ops | PASS   | `op: add, path: /metadata/annotations/migrated-by, value: crane` |
| CRB patches include annotation add ops         | PASS   | Same ops present                                                 |
| Deployment patches include annotation add ops  | PASS   | Same ops present                                                 |
| kubectl kustomize builds successfully          | PASS   | All 10 resources get both annotations                            |
| Apply output has annotations on cluster-scoped | PASS   | `migrated-by: crane` in ClusterRole output file                  |


**Status: PASS**

---

## T6: CRB with Subjects in Multiple Namespaces

**Goal:** Verify ClusterRoleBinding with subjects pointing to ServiceAccounts in two different namespaces is preserved through the pipeline.

**Setup:** `crane-test-cross-ns-binding` has subjects:

- `app-sa` in `crane-cluster-test`
- `cross-ns-sa` in `crane-cluster-test-2`

**Results (traced through all 3 stages):**


| Stage                     | Subject 1 namespace  | Subject 2 namespace    | Correct? |
| ------------------------- | -------------------- | ---------------------- | -------- |
| Export                    | `crane-cluster-test` | `crane-cluster-test-2` | PASS     |
| Transform (resource file) | `crane-cluster-test` | `crane-cluster-test-2` | PASS     |
| Apply output              | `crane-cluster-test` | `crane-cluster-test-2` | PASS     |


Transform does not modify CRB subjects — they pass through unchanged. This is correct behavior since namespace remapping is not implemented and would require a separate feature.

**Status: PASS**

---

## T7: Re-run Transform with --force Flag

**Goal:** Verify --force correctly overwrites existing transform output including cluster-scoped resources.

**Results:**


| Check                                          | Result | Detail                                                           |
| ---------------------------------------------- | ------ | ---------------------------------------------------------------- |
| Second run WITHOUT --force fails               | PASS   | Error: "stage directory is not empty (use --force to overwrite)" |
| Second run WITH --force succeeds               | PASS   | Same 17 files regenerated                                        |
| Cluster-scoped resources present after --force | PASS   | Both resource type files present                                 |


**Status: PASS**

---

## T8: kubectl kustomize Build Validation

**Goal:** Verify the generated kustomization.yaml is valid and kubectl can process it.

**Results:**


| Variant                    | Build succeeds | Resources in output                           | Metadata clean               |
| -------------------------- | -------------- | --------------------------------------------- | ---------------------------- |
| T1 (single-stage)          | PASS           | 10 (2 CR, 3 CRB, 1 CM, 1 Deploy, 1 Svc, 2 SA) | PASS (0 uid/resourceVersion) |
| T2b (multi-stage, stage 2) | PASS           | 10                                            | PASS                         |
| T5 (with annotations)      | PASS           | 10, all with `migrated-by` annotation         | PASS                         |


**Status: PASS**

---

## Bugs Found

### BUG-1: Multi-stage transform silently skips all plugins when stage name doesn't match a plugin name

**Severity:** High
**Component:** `internal/transform/orchestrator.go` — `RunMultiStage()` / `executeStage()`

**Description:** When running multi-stage transform, the runner extracts the plugin name from the stage directory pattern `<number>_<pluginName>`. If the `<pluginName>` portion doesn't match any available plugin, all resources pass through completely raw — no metadata stripping, no whiteouts, no patches generated.

**No warning or error is emitted.** The command exits 0 with "Successfully completed N stage(s)".

**Reproduction:**

```bash
crane export -n <namespace>
crane transform --stage-name "10_MyCustomStage"
mkdir -p transform/20_AnotherStage
crane transform --from-stage "10_MyCustomStage" --to-stage "20_AnotherStage" --force
# Result: both stages have 0 patches, all metadata intact, no whiteouts
```

**Impact:** Users who create stages with descriptive names (rather than plugin names) get silently broken output. The transform appears to succeed but produces raw, untransformed resources.

**Expected behavior:** Either:

- (a) Error/warn when no plugins match a stage's derived plugin name, OR
- (b) Run all available plugins by default when no specific plugin matches, OR
- (c) Document that stage names MUST use the pattern `<number>_<ExactPluginName>`

---

### OBSERVATION-1: `kubectl.kubernetes.io/last-applied-configuration` annotation not stripped

**Severity:** Low (not a bug, but worth noting)
**Component:** crane-lib `KubernetesTransformPlugin`

**Description:** The `last-applied-configuration` annotation contains a full JSON snapshot of the resource as originally applied, including hardcoded namespace references and source-cluster-specific data. It is preserved through the entire pipeline. For cluster-scoped resources like ClusterRoleBindings, this means the annotation still contains the source namespace in the embedded JSON.

**Not blocking:** This is existing behavior for all resources and doesn't affect functional correctness. The annotation is informational and kubectl will overwrite it on the next `kubectl apply`.

---

## Summary


| Test                                 | Status      | Notes                                                        |
| ------------------------------------ | ----------- | ------------------------------------------------------------ |
| T1: Basic single-stage pipeline      | PASS        | Full end-to-end works                                        |
| T2: Multi-stage transform            | **PARTIAL** | Works with correct plugin names; BUG-1 with mismatched names |
| T3: Mixed resource patches           | PASS        | No cross-contamination                                       |
| T4: Patch filename uniqueness        | PASS        | No collisions                                                |
| T5: Optional flags on cluster-scoped | PASS        | Annotations work equally                                     |
| T6: Multi-namespace CRB subjects     | PASS        | Both namespaces preserved                                    |
| T7: --force flag                     | PASS        | Correct overwrite behavior                                   |
| T8: kubectl kustomize build          | PASS        | Valid kustomization.yaml                                     |


**Overall:** The transform & apply pipeline handles cluster-scoped resources correctly for the single-stage (default) flow. The one significant bug found (BUG-1) is in the multi-stage flow and is not specific to cluster-scoped resources — it affects all resources when stage names don't match plugin names.