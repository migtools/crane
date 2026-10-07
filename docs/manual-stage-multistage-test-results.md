# Manual Stage & Multi-Stage Flags: Test Results

**Date:** 2026-04-10
**Issue context:** [migtools/crane#189](https://github.com/migtools/crane/issues/189)
**Crane version:** v0.0.6 | crane-lib: v0.0.10
**Cluster:** minikube (Kubernetes)
**Binary:** built from `main` branch (commit aa106d6)

---

## Test Approach

All tests use **manually created stage directories** with hand-written `kustomization.yaml`, `resources/`, and `patches/` — no custom Go binary plugins. This simulates a user who creates custom kustomize stages alongside crane-generated stages.

## Test Fixture

**Namespace:** `crane-manual-test`

Resources: ServiceAccount, ConfigMap, Service, Deployment, ClusterRole (`crane-manual-test-role`), ClusterRoleBinding (`crane-manual-test-binding`).

**Base stage:** `crane transform` produced `transform/10_KubernetesPlugin/` with 6 patches and 6 resources (whiteouts applied).

---

## M1: Custom Stage After KubernetesPlugin — Manual Annotation Patches

**Goal:** Can a user create a manual stage 2 with hand-written patches on top of crane-generated stage 1?

**Setup:** Created `transform/20_CustomAnnotations/` with:
- `resources/all.yaml` — stage 1 built output (via `kubectl kustomize`)
- `patches/add-deploy-annotation.patch.yaml` — adds `migrated-by: crane-manual` to Deployment
- `patches/add-cr-annotation.patch.yaml` — adds `migrated-by: crane-manual` to ClusterRole
- `kustomization.yaml` — references both

**Command:** `crane apply`

**Result:**
- Deployment: `migrated-by: crane-manual` present ✅
- ClusterRole: `migrated-by: crane-manual` present ✅
- ClusterRoleBinding: no `migrated-by` (not targeted) ✅

**Status: PASS**

---

## M2: apply --stage — Select Only One Stage

**Goal:** Does `apply --stage` correctly build only one stage?

**Commands:**
```bash
crane apply --stage 10_KubernetesPlugin    # only stage 1
crane apply --stage 20_CustomAnnotations   # only stage 2
```

**Results:**
- `--stage 10` → output has no custom annotations (stage 2 skipped) ✅
- `--stage 20` → output has custom annotations ✅
- Output file named `<stageName>.yaml` (not `output.yaml`)
- No `resources/` split directory created (unlike default apply)

**OBSERVATION-4:** `apply --stage` outputs a single `<stageName>.yaml` file without the `resources/` directory split. Default `apply` (final stage) creates both `output.yaml` AND `resources/<namespace>/*.yaml`. Inconsistent output format between the two modes.

**Status: PASS (with observation)**

---

## M3: --stages with Non-Contiguous Selection

**Goal:** Does `--stages 10,30` (skipping 20) work correctly?

**Setup:** Three stages: `10_KubernetesPlugin`, `20_AddAnnotation`, `30_AddLabel`

**Command:** `crane apply --stages "10_KubernetesPlugin,30_AddLabel"`

**Result:** Stages 10 and 30 built independently. Stage 20 skipped. Each produces its own YAML file. Stage 30's output contains its own patches applied to its own resources (which I had pre-built from stage 2 output).

**Status: PASS**

---

## M4: Custom Stage BEFORE KubernetesPlugin — Multi-Stage Chain

**Goal:** Can a user put a custom stage (05) before KubernetesPlugin (10), then run multi-stage to chain them?

**Setup:** Created `transform/05_PreAnnotation/` with hand-written kustomization.yaml, resources (raw export files), and a patch adding `pre-transform: true` to ClusterRoleBinding. Created empty `transform/10_KubernetesPlugin/`.

**Command:**
```bash
crane transform --from-stage 05_PreAnnotation --to-stage 10_KubernetesPlugin --force
```

**Result:**

**BUG-5: Multi-stage transform with --force destroys manually created custom stages**

The `--force` flag caused the multi-stage runner to:
1. Delete `05_PreAnnotation/` entirely (removing user's hand-written kustomization.yaml, patches, resources)
2. Re-create it with raw pass-through output (no plugin named "PreAnnotation" matched → 0 patches)
3. The user's custom patch `pre-annotation.patch.yaml` was destroyed

**Before --force:**
```
05_PreAnnotation/
  kustomization.yaml    ← user-written, 12 resources + 1 patch
  patches/
    pre-annotation.patch.yaml    ← user-written
  resources/
    12 export files
```

**After --force:**
```
05_PreAnnotation/
  kustomization.yaml    ← re-generated, 10 resources + 0 patches
  patches/              ← empty
  resources/
    10 resource type files (re-grouped)
```

**Status: FAIL (BUG-5)**

---

## M5: --from-stage Only (No --to-stage)

**Goal:** What does `--from-stage` do without `--to-stage`?

**Command:** `crane apply --from-stage 20_AddAnnotation`

**Result:** Runs from stage 20 to the last stage (30). Stage 10 skipped. Correct behavior.

**Status: PASS**

---

## M6: --to-stage Only (No --from-stage)

**Goal:** What does `--to-stage` do without `--from-stage`?

**Command:** `crane apply --to-stage 20_AddAnnotation`

**Result:** Runs from first stage (10) to stage 20. Stage 30 skipped. Correct behavior.

**Status: PASS**

---

## M7: Custom Patch Targeting Non-Existent Resource

**Goal:** What happens when a patch targets a resource that doesn't exist (wrong name)?

**Setup:** Patch targets `Deployment/nonexistent-app` — no such resource exists.

**Command:** `crane apply`

**Result:** Exit 0, no error, no warning. Patch silently ignored. All 6 resources output without the patch.

**OBSERVATION-5:** A patch targeting a non-existent resource is silently ignored by `kubectl kustomize`. This is kustomize's behavior, not crane's, but a user with a typo in their patch target name would get no feedback that their patch had no effect.

**Status: PASS (with observation — kustomize behavior)**

---

## M8: apply --from-stage --to-stage Chaining

**Goal:** Does apply chain stages (pipe output of stage N into stage N+1)?

**Command:** `crane apply --from-stage 10_KubernetesPlugin --to-stage 20_AddAnnotation`

**Result:** Each stage builds independently from its own `kustomization.yaml`. Apply does NOT chain stages — it runs `kubectl kustomize` on each stage directory separately and writes individual output files.

**Status: PASS**

---

## M9: --force on Specific Stage

**Goal:** Does `transform --stage X --force` only re-run stage X and leave others untouched?

**Setup:** Stage 10 (KubernetesPlugin) + stage 20 (manual custom patches).

**Command:** `crane transform --stage 10_KubernetesPlugin --force`

**Result:** Stage 10 re-generated. Stage 20's custom patches, resources, and kustomization.yaml preserved.

**Status: PASS** — `--stage` with `--force` correctly scopes to only the selected stage.

---

## M10: Invalid Stage Names and Edge Cases

| Case | Command | Result | Correct? |
|------|---------|--------|----------|
| A | `apply --stage 99_DoesNotExist` | Error: "no stages found matching selector", exit 1 | ✅ |
| B | `apply --from-stage 99_DoesNotExist` | Error: "no stages found matching selector", exit 1 | ✅ |
| C | `apply --stages "10_KubernetesPlugin,99_DoesNotExist"` | Exit 0, only valid stage processed | ⚠️ |
| D | `transform --stage X --from-stage X` | Error: "mutually exclusive", exit 1 | ✅ |

**OBSERVATION-6:** `--stages` with a mix of valid and invalid stage names silently drops the invalid ones and processes only the valid ones (exit 0, no warning). A typo in one stage name would go unnoticed.

---

## Bugs Found

### BUG-5: Multi-stage transform with --force destroys manually created custom stages

**Severity:** High
**Component:** `internal/transform/orchestrator.go` — `RunMultiStage()` + `executeStage()`

**Description:** When `--from-stage` / `--to-stage` is used with `--force`, the multi-stage runner re-executes ALL stages in the range. For each stage, `--force` causes `os.RemoveAll(stageDir)` (via `WriteStage` in writer.go), deleting the entire directory including any hand-written content. The stage is then re-created with whatever the matching plugin produces (or raw pass-through if no plugin matches).

This makes custom manually-created stages incompatible with `--force` in multi-stage mode. A user who creates a custom stage and later needs to re-run a different stage with `--force` must be very careful not to include the custom stage in the range.

**Workaround:** Use `--stage <specific_stage> --force` instead of `--from-stage/--to-stage --force` to scope the re-run to only one stage (verified in M9).

**Reproduction:**
```bash
# Create custom stage with hand-written patches
mkdir -p transform/05_Custom/resources transform/05_Custom/patches
# ... add resources, patches, kustomization.yaml ...

# Create plugin stage
mkdir -p transform/10_KubernetesPlugin

# This DESTROYS the custom stage:
crane transform --from-stage 05_Custom --to-stage 10_KubernetesPlugin --force
```

---

## Observations

### OBSERVATION-4: Inconsistent output format between apply modes

- Default `crane apply` → creates `output/output.yaml` + `output/resources/<namespace>/*.yaml` + `output/resources/_cluster/*.yaml`
- `crane apply --stage X` → creates only `output/<stageName>.yaml` (no `resources/` split)

### OBSERVATION-5: Kustomize silently ignores patches targeting non-existent resources

A patch with a target selector that matches no resource produces no error — the patch is silently skipped. Typos in target name/kind/namespace go unnoticed.

### OBSERVATION-6: --stages silently drops invalid stage names

`--stages "valid,invalid"` processes only the valid stage with exit 0. No warning about the unrecognized stage name.

---

## Summary

| Test | Status | Notes |
|------|--------|-------|
| M1: Custom stage after K8s plugin | PASS | Hand-written patches work correctly |
| M2: apply --stage single select | PASS | Correct isolation (OBSERVATION-4: output format differs) |
| M3: --stages non-contiguous | PASS | Independent builds |
| M4: Custom stage before K8s plugin + --force | **FAIL** | BUG-5: custom stage destroyed |
| M5: --from-stage only | PASS | Runs from stage to last |
| M6: --to-stage only | PASS | Runs from first to stage |
| M7: Patch targeting non-existent resource | PASS | Silently ignored (OBSERVATION-5) |
| M8: apply --from-stage --to-stage | PASS | Independent builds, no chaining |
| M9: --stage --force (scoped) | PASS | Only re-runs selected stage |
| M10: Invalid stage names | PASS | Errors on single invalid, but --stages silently drops (OBSERVATION-6) |

**New bugs found:** 1 (BUG-5)
**New observations:** 3 (OBSERVATION-4, OBSERVATION-5, OBSERVATION-6)
**Cumulative totals across all sessions:** 5 bugs, 6 observations
