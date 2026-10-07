# Plugin & Multi-Stage Transform: Test Results

**Date:** 2026-04-10
**Issue context:** [migtools/crane#189](https://github.com/migtools/crane/issues/189)
**Crane version:** v0.0.6 | crane-lib: v0.0.10
**Cluster:** minikube (Kubernetes)
**Binary:** built from `main` branch (commit aa106d6)

---

## Custom Plugins Built

| Plugin Name | Behavior | Purpose |
|---|---|---|
| `AnnotationPlugin` | Adds `test-annotation: added-by-annotation-plugin` to every resource | Test patching, ordering, optional flags |
| `LabelPlugin` | Adds `migration-stage: labeled-by-label-plugin` label to every resource | Test patch validation edge cases |
| `WhiteoutCRBPlugin` | Whiteouts ClusterRoleBindings | Test selective cluster-scoped whiteout |
| `WhiteoutClusterScopedPlugin` | Whiteouts any resource without a namespace | Test aggressive cluster-scope removal |
| `ConflictPlugin` | Adds `test-annotation: from-conflict-plugin` (same path as AnnotationPlugin) | Test priority/conflict resolution |

All plugins built as standalone Go binaries using `crane-lib/transform/cli` package, placed in `/tmp/crane-tests/plugins/`.

## Test Fixture

**Namespace:** `crane-plugin-test`

Resources: 1 ServiceAccount, 1 ConfigMap, 1 Service, 1 Deployment, 1 ClusterRole, 1 ClusterRoleBinding (12 total resources after export including auto-generated ones).

---

## P1: AnnotationPlugin First, KubernetesPlugin Second

**Goal:** Does stage 2 (KubernetesPlugin) correctly process resources that stage 1 (AnnotationPlugin) already patched?

**Commands:**
```bash
crane transform -p $PDIR --stage-name "10_AnnotationPlugin"
mkdir -p transform/20_KubernetesPlugin
crane transform -p $PDIR --from-stage 10_AnnotationPlugin --to-stage 20_KubernetesPlugin --force
crane apply
```

**Results:**
- Stage 1: 12 patches (annotation added to every resource), all resources present (no whiteouts) — correct, AnnotationPlugin doesn't whiteout
- Stage 2: 6 patches (metadata removal), 6 resources (Endpoints/Pod/ReplicaSet/EndpointSlice whiteout'd) — correct
- Final output: annotation present + metadata stripped on all resources including cluster-scoped

**Status: PASS**

---

## P2: KubernetesPlugin First, AnnotationPlugin Second

**Goal:** Does stage 2 (AnnotationPlugin) correctly add annotations to already-cleaned resources from stage 1?

**Commands:**
```bash
crane transform -p $PDIR --stage-name "10_KubernetesPlugin"
mkdir -p transform/20_AnnotationPlugin
crane transform -p $PDIR --from-stage 10_KubernetesPlugin --to-stage 20_AnnotationPlugin --force
crane apply
```

**Results:**
- Stage 1: 6 patches + whiteouts — correct
- Stage 2: 6 patches (annotation added), 6 resources — correct
- Inter-stage handoff via `kubectl kustomize`: patches from stage 1 applied before stage 2 reads them
- Final output: metadata clean + annotation present

**Status: PASS**

---

## P3: Three Plugins in Sequence (3-stage)

**Goal:** Does a deep pipeline (KubernetesPlugin → AnnotationPlugin → LabelPlugin) work?

**Commands:**
```bash
crane transform -p $PDIR --stage-name "10_KubernetesPlugin"
mkdir -p transform/20_AnnotationPlugin transform/30_LabelPlugin
crane transform -p $PDIR --from-stage 10_KubernetesPlugin --to-stage 30_LabelPlugin --force
crane apply
```

**Results:**
- Stages 1 and 2 execute correctly
- Stage 3 generates patches, transform completes successfully
- **Apply FAILS:** `error: add operation does not apply: doc is missing path: "/metadata/labels/migration-stage": missing value`

**BUG-2: Transform accepts and writes patches that reference non-existent parent paths**

The LabelPlugin generates `{"op": "add", "path": "/metadata/labels/migration-stage", "value": "..."}`, but many resources (ConfigMap, Service, ServiceAccount, ClusterRole, ClusterRoleBinding) don't have a `metadata.labels` object. Per RFC 6902, `add` only creates the leaf node, not intermediate path segments.

Transform writes these invalid patches without any validation. The error only surfaces when `kubectl kustomize` attempts to apply them during the apply step.

**Impact:**
- Silent invalid patches in transform output
- Error appears far from root cause (apply, not transform)
- Cluster-scoped resources are especially vulnerable since they rarely have labels
- `filterValidRemoveOps()` in writer.go validates `remove` ops but there's no equivalent validation for `add` ops

**Status: FAIL (BUG-2)**

---

## P4: Whiteout CRBs in Stage 2

**Goal:** Can a custom plugin selectively whiteout cluster-scoped resources mid-pipeline?

**Commands:**
```bash
crane transform -p $PDIR --stage-name "10_KubernetesPlugin"
mkdir -p transform/20_WhiteoutCRBPlugin
crane transform -p $PDIR --from-stage 10_KubernetesPlugin --to-stage 20_WhiteoutCRBPlugin --force
crane apply
```

**Results:**
- Stage 1: normal processing
- Stage 2: CRB whiteout'd, ClusterRole preserved
- Apply: output has `_cluster/ClusterRole_*.yaml` but no CRB files
- No `_cluster/` CRB entries in output.yaml

**Status: PASS**

---

## P5: Whiteout All Cluster-Scoped Resources in Stage 2

**Goal:** Can all cluster-scoped resources be filtered out by a plugin?

**Commands:**
```bash
crane transform -p $PDIR --stage-name "10_KubernetesPlugin"
mkdir -p transform/20_WhiteoutClusterScopedPlugin
crane transform -p $PDIR --from-stage 10_KubernetesPlugin --to-stage 20_WhiteoutClusterScopedPlugin --force
crane apply
```

**Results:**
- Both ClusterRole and ClusterRoleBinding removed
- No `_cluster/` directory in output
- Only namespaced resources remain

**Status: PASS**

---

## P6: Single-Stage with Custom Plugin Only (--plugin-name)

**Goal:** Does `--plugin-name AnnotationPlugin` isolate to only that plugin?

**Command:**
```bash
crane transform -p $PDIR --plugin-name AnnotationPlugin
```

**Results:**
- Only AnnotationPlugin ran — annotation patches only, no metadata stripping, no whiteouts
- All 12 resources present (including Endpoints, Pods, etc.)
- Correct isolation

**OBSERVATION-2: Default --stage-name is misleading**

The default `--stage-name` is `10_KubernetesPlugin`, so the output directory is named `10_KubernetesPlugin` even though only `AnnotationPlugin` ran. This is confusing:
- User sees `transform/10_KubernetesPlugin/` in their filesystem
- In multi-stage mode, this directory name would cause the system to look for KubernetesPlugin, not AnnotationPlugin
- A single-stage run with `--plugin-name X` followed by a multi-stage run on the same directory will run the wrong plugin

**Status: PASS (with observation)**

---

## P7: --skip-plugins in Multi-Stage

**Goal:** Does `--skip-plugins KubernetesPlugin` work in multi-stage mode?

**Command:**
```bash
mkdir -p transform/10_KubernetesPlugin transform/20_AnnotationPlugin
crane transform -p $PDIR -s KubernetesPlugin --from-stage 10_KubernetesPlugin --to-stage 20_AnnotationPlugin --force
```

**Results:**
- Stage 1 (`10_KubernetesPlugin`): 0 patches, all resources raw — KubernetesPlugin was skipped, and since the stage filters by plugin name "KubernetesPlugin" which is now skipped, no plugins match
- Stage 2 (`20_AnnotationPlugin`): 12 patches — AnnotationPlugin ran on all raw resources

**OBSERVATION-3: --skip-plugins interacts poorly with multi-stage naming**

When `--skip-plugins KubernetesPlugin` is used but a stage is named `10_KubernetesPlugin`, the stage silently produces raw output (0 patches, no whiteouts). Same silent pass-through as BUG-1 from the previous test session. The user gets no warning that stage 1 effectively did nothing.

**Status: PASS (with observation)**

---

## P8: All Plugins in Single Stage (no --plugin-name)

**Goal:** Do all loaded plugins run together in a single stage?

**Command:**
```bash
crane transform -p $PDIR
```

**Results:**
- All 6 plugins ran on each resource
- WhiteoutCRBPlugin + WhiteoutClusterScopedPlugin whiteout'd all cluster-scoped resources
- Surviving resources got combined patches from KubernetesPlugin + AnnotationPlugin + LabelPlugin + ConflictPlugin
- ConflictPlugin vs AnnotationPlugin conflict resolved (one won, based on internal ordering)
- Apply fails due to BUG-2 (LabelPlugin's add to non-existent `/metadata/labels`)

**Status: PARTIAL PASS (plugin execution correct, apply fails due to BUG-2)**

---

## P9: Plugin Conflict — Priority Resolution

**Goal:** Does `--plugin-priorities` correctly resolve conflicts when two plugins patch the same path?

**Commands:**
```bash
# ConflictPlugin prioritized
crane transform -p $PDIR --skip-plugins KubernetesPlugin,WhiteoutCRBPlugin,WhiteoutClusterScopedPlugin,LabelPlugin \
    --plugin-priorities ConflictPlugin --ignored-patches-dir ignored

# AnnotationPlugin prioritized (separate run)
crane transform -p $PDIR --skip-plugins KubernetesPlugin,WhiteoutCRBPlugin,WhiteoutClusterScopedPlugin,LabelPlugin \
    --plugin-priorities AnnotationPlugin --force
```

**Results:**
- When ConflictPlugin prioritized: patch value is `from-conflict-plugin` — correct
- When AnnotationPlugin prioritized: patch value is `added-by-annotation-plugin` — correct
- Priority resolution works for both namespaced and cluster-scoped resources

**BUG-3: `--ignored-patches-dir` never writes any files**

The `--ignored-patches-dir` flag is accepted but never produces output. The root cause is in `orchestrator.go:86`:
```go
IgnoredOps: []cranelib.IgnoredOperation{}, // TODO: Parse IgnoredPatches
```

The `RunnerResponse.IgnoredPatches` from the runner is never parsed and forwarded to the `TransformArtifact`. The losing side of a conflict is silently discarded with no record.

**Impact:** Users have no way to audit which plugin operations were rejected due to conflicts. They must use `--debug` and read log output to see conflict resolution details.

**Status: PASS (priority works) with BUG-3 (ignored patches not written)**

---

## P10: Non-Existent Plugin Directory

**Goal:** What happens with `--plugin-dir` pointing to a non-existent directory?

**Command:**
```bash
crane transform -p /tmp/nonexistent-plugin-dir
```

**Results:**
- No error, exits 0
- Only built-in KubernetesPlugin runs
- Correct whiteouts and metadata stripping

This is correct behavior — `GetPlugins()` handles `os.IsNotExist` gracefully and falls back to built-in plugins.

**Status: PASS**

---

## P11: list-plugins and optionals Subcommands

**Goal:** Do discovery subcommands show custom plugins?

**Commands:**
```bash
crane transform list-plugins -p $PDIR
crane transform optionals -p $PDIR
```

**Results:**
- `list-plugins`: All 6 plugins listed with names and versions
- `optionals`: Shows optional fields for KubernetesPlugin (9 fields) and AnnotationPlugin (1 field)
- Plugins without optional fields (LabelPlugin, etc.) show no optional fields section

**Status: PASS**

---

## Bugs Found

### BUG-2: Transform writes invalid patches without validation (add ops to non-existent paths)

**Severity:** Medium
**Component:** `internal/transform/writer.go` — `WriteStage()`
**Reproduction:** Any plugin that generates `{"op": "add", "path": "/metadata/labels/something"}` on a resource without `metadata.labels`

**Description:** Transform writes all patches to disk without validating that `add` operations have valid parent paths. The existing `filterValidRemoveOps()` function validates `remove` operations (checking the path exists before writing), but there is no equivalent for `add` operations.

The error only surfaces at `kubectl kustomize` time during apply:
```
error: add operation does not apply: doc is missing path: "/metadata/labels/migration-stage": missing value
```

**Root cause:** RFC 6902 `add` requires the parent container to exist. `filterValidRemoveOps` was written to catch invalid remove ops, but no `filterValidAddOps` exists.

**Who is affected:** Any custom plugin that adds fields to paths that may not exist on all resources. Cluster-scoped resources are especially vulnerable since they often lack labels, annotations beyond `last-applied-configuration`, or other optional metadata.

---

### BUG-3: --ignored-patches-dir never produces output

**Severity:** Low-Medium
**Component:** `internal/transform/orchestrator.go:86,215`

**Description:** The `--ignored-patches-dir` flag is accepted but the directory is never populated. In both `RunSingleStage` and `executeStage`, the `TransformArtifact.IgnoredOps` is hardcoded to an empty slice:

```go
IgnoredOps: []cranelib.IgnoredOperation{}, // TODO: Parse IgnoredPatches
```

The `RunnerResponse.IgnoredPatches` from `crane-lib` contains the serialized losing operations from conflict resolution, but this data is never deserialized and forwarded.

**Impact:** Users cannot audit plugin conflicts. The flag exists in help text but does nothing.

---

### BUG-4: --plugin-name silently ignored in multi-stage mode

**Severity:** Medium
**Component:** `cmd/transform/transform.go:184-200`

**Description:** When any multi-stage flag (`--stage`, `--from-stage`, `--to-stage`, `--stages`) is used together with `--plugin-name`, the `--plugin-name` flag is silently discarded. No warning or error is emitted. The plugin that actually runs is derived from the stage directory name, not from `--plugin-name`.

**Reproduction:**
```bash
crane export -n <namespace>
mkdir -p transform/10_KubernetesPlugin

# User explicitly asks for AnnotationPlugin on this stage
crane transform -p <plugin-dir> --plugin-name AnnotationPlugin --stage 10_KubernetesPlugin --force
```

**Output (no warning):**
```
level=info msg="Running multi-stage transform"
level=info msg="Executing stage 1/1: 10_KubernetesPlugin"
level=info msg="Successfully completed 1 stage(s)"
```

**Actual result:** KubernetesPlugin ran (metadata stripping patches), not AnnotationPlugin. Verified by inspecting patches:
```yaml
- op: remove
  path: /metadata/uid
- op: remove
  path: /metadata/resourceVersion
```

**Root cause:** In `transform.go`, the presence of `--stage` (or `--from-stage`/`--to-stage`/`--stages`) causes the code to branch to `RunMultiStage()` which never reads `o.PluginName`. Only `RunSingleStage()` receives the `pluginName` parameter:

```go
if o.Stage != "" || o.FromStage != "" || o.ToStage != "" || len(o.Stages) > 0 {
    return orchestrator.RunMultiStage(selector)  // pluginName not passed
}
return orchestrator.RunSingleStage(o.StageName, o.PluginName)  // only here
```

**Context:** Transform creates the stage directories and needs to know which plugin to run. Apply doesn't run plugins at all (confirmed: no `--plugin-name` in apply flags). So `--plugin-name` is only relevant during transform — but it should work in both single-stage and multi-stage modes, or at minimum warn when it will be ignored.

**Expected behavior:** Either:
- (a) Error/warn when `--plugin-name` is combined with multi-stage flags
- (b) Use `--plugin-name` to override the directory-derived plugin name for the selected stages

---

## Observations (User-Unfriendly Behavior)

### OBSERVATION-2: Default --stage-name is misleading

The default value of `--stage-name` is `10_KubernetesPlugin`. When running with `--plugin-name AnnotationPlugin`, the output directory is still named `10_KubernetesPlugin`. This causes confusion:
- The filesystem suggests KubernetesPlugin ran, but it didn't
- If this directory is later used in multi-stage mode, the system will try to match plugin "KubernetesPlugin" from the directory name, not "AnnotationPlugin"

**Suggestion:** Default `--stage-name` could derive from `--plugin-name` when specified, e.g. `10_AnnotationPlugin`.

### OBSERVATION-3: --skip-plugins + multi-stage = silent no-op stages

When `--skip-plugins` removes the plugin that a stage is named after, that stage silently produces raw output. Combined with BUG-1 from the previous test session (stage names not matching plugin names), this creates a pattern where stages can silently fail to transform resources.

---

## Summary

| Test | Status | Notes |
|------|--------|-------|
| P1: Custom first, K8s second | PASS | Inter-stage data flow works |
| P2: K8s first, custom second | PASS | Clean resources get new patches |
| P3: 3-stage pipeline | **FAIL** | BUG-2: invalid add patches pass transform, fail at apply |
| P4: Whiteout CRBs mid-pipeline | PASS | Selective cluster-scoped whiteout works |
| P5: Whiteout all cluster-scoped | PASS | Aggressive filtering works |
| P6: Single plugin isolation | PASS | --plugin-name isolates correctly (OBSERVATION-2) |
| P7: --skip-plugins multi-stage | PASS | Skip works but silent no-op (OBSERVATION-3) |
| P8: All plugins single stage | **PARTIAL** | Multi-plugin execution correct, apply fails (BUG-2) |
| P9: Plugin conflict priority | PASS | Priority resolution correct (BUG-3: ignored patches not saved) |
| P10: Non-existent plugin dir | PASS | Graceful fallback to built-in |
| P11: list-plugins/optionals | PASS | Custom plugins discovered correctly |

**New bugs found:** 3 (BUG-2, BUG-3, BUG-4)
**New observations:** 2 (OBSERVATION-2, OBSERVATION-3)
**Total across both test sessions:** 4 bugs, 3 observations
