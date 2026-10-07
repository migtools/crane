# Issue #189: Update Transform & Apply for Cluster-Level Resources

## Context

Crane's `export` command already correctly discovers and writes cluster-scoped resources (ClusterRole, ClusterRoleBinding, CRD, SCC) to `export/resources/<ns>/_cluster/`. The `transform` and `apply` commands have **partial** support for cluster-scoped resources — the core plumbing works (recursive file loading, filename generation with "clusterscoped" placeholder, patch target with omitted namespace, `_cluster/` output directory routing). However, there are no comprehensive tests proving correctness, no `--skip-cluster-scoped` flag, and no E2E validation.

This plan addresses [issue #189](https://github.com/migtools/crane/issues/189) in two phases.

## Current State Analysis

### What already works

| Area | Status | Details |
|------|--------|---------|
| **Export** cluster-scoped discovery | Done | ClusterRole, ClusterRoleBinding, CRD, SCC discovered and written to `_cluster/` |
| **Transform** recursive file loading | Done | `ReadFiles()` traverses `_cluster/` subdirectories |
| **Transform** resource ID for cluster-scoped | Done | `getResourceID()` returns `Kind/name` (no namespace) |
| **Transform** filename generation | Done | `GetResourceFilename()` uses `"clusterscoped"` placeholder |
| **Transform** patch target namespace omission | Done | `PatchTarget.Namespace` uses `omitempty` in YAML serialization |
| **Transform** patch filename for cluster-scoped | Done | `GeneratePatchFilename()` omits namespace prefix |
| **Transform** inter-stage `_cluster/` routing | Done | `writeResourcesToDirectory()` routes to `_cluster/` subdirectory |
| **Apply** output split to `_cluster/` | Done | `splitMultiDocYAMLToFiles()` routes `namespace==""` to `_cluster/` |
| **Apply** basic test coverage | Partial | Tests for "cluster-scoped resource" and "mixed" exist in `kustomize_test.go` |

### What's missing (the gaps)

1. **`--skip-cluster-scoped` flag** on `crane apply` (requirement 9 from issue)
2. **Comprehensive unit tests** proving transform handles cluster-scoped resources (writer, orchestrator)
3. **Integration validation** that `kubectl kustomize` succeeds with mixed namespaced + cluster-scoped content
4. **E2E tests** for RBAC and CRD migration end-to-end
5. **Whiteout validation** for cluster-scoped resources

### Pipeline flow example (current behavior)

```
# Export creates:
export/resources/my-app/
  Deployment_apps_v1_my-app_web.yaml
  Service__v1_my-app_web-svc.yaml
  _cluster/
    ClusterRole_rbac.authorization.k8s.io_v1_clusterscoped_web-admin.yaml
    ClusterRoleBinding_rbac.authorization.k8s.io_v1_clusterscoped_web-admin-binding.yaml

# Transform reads all files recursively, runs plugins, writes stage:
transform/10_KubernetesPlugin/
  resources/
    Deployment_apps_v1_my-app_web.yaml
    Service__v1_my-app_web-svc.yaml
    ClusterRole_rbac.authorization.k8s.io_v1_clusterscoped_web-admin.yaml
    ClusterRoleBinding_rbac.authorization.k8s.io_v1_clusterscoped_web-admin-binding.yaml
  patches/
    my-app--apps-v1--Deployment--web.patch.yaml
    rbac.authorization.k8s.io-v1--ClusterRole--web-admin.patch.yaml
  kustomization.yaml

# Inter-stage working directory:
transform/.work/10_KubernetesPlugin/output/
  my-app/
    Deployment_apps_v1_my-app_web.yaml
    Service__v1_my-app_web-svc.yaml
  _cluster/
    ClusterRole_rbac.authorization.k8s.io_v1_clusterscoped_web-admin.yaml
    ClusterRoleBinding_rbac.authorization.k8s.io_v1_clusterscoped_web-admin-binding.yaml

# Apply produces:
output/
  output.yaml
  resources/
    my-app/
      Deployment_apps_v1_my-app_web.yaml
      Service__v1_my-app_web-svc.yaml
    _cluster/
      ClusterRole_rbac.authorization.k8s.io_v1_clusterscoped_web-admin.yaml
      ClusterRoleBinding_rbac.authorization.k8s.io_v1_clusterscoped_web-admin-binding.yaml
```

---

## Phase 1: Transform — Manual Testing, Bug Fixes, Unit Tests

### Step 1.1: Manual Testing Plan on Minikube

**Goal**: Run the full export → transform pipeline with cluster-scoped resources on real minikube clusters to discover bugs before writing tests.

**Prerequisites**: src/tgt minikube clusters already running with contexts.

#### Test Case 1 (Positive): App with ClusterRole + ClusterRoleBinding

1. Create namespace `crane-rbac-test` on src cluster
2. Deploy a simple nginx Deployment + ServiceAccount
3. Create a ClusterRole `crane-test-reader` with read permissions
4. Create a ClusterRoleBinding `crane-test-reader-binding` binding the SA to the ClusterRole
5. Run `crane export -n crane-rbac-test`
6. **Verify**: `export/resources/crane-rbac-test/_cluster/` contains ClusterRole and ClusterRoleBinding files
7. Run `crane transform -e export -t transform`
8. **Verify**:
   - `transform/10_KubernetesPlugin/resources/` contains ClusterRole and ClusterRoleBinding files with `clusterscoped` in filename
   - `kustomization.yaml` lists them in `resources:` section
   - Any patches for cluster-scoped resources have no `namespace:` in target
   - `kubectl kustomize transform/10_KubernetesPlugin/` succeeds and outputs cluster-scoped resources

#### Test Case 2 (Positive): App with CRD + Custom Resource

1. Create a simple test CRD (e.g., `widgets.example.com`) on src cluster
2. Create namespace `crane-crd-test` and a Widget CR instance in it
3. Run `crane export -n crane-crd-test`
4. **Verify**: CRD appears in `_cluster/` directory
5. Run `crane transform` and verify same checks as Test Case 1

#### Test Case 3 (Positive): Mixed namespaced + cluster-scoped through multi-stage

1. Same setup as Test Case 1
2. Run transform, then verify inter-stage output in `.work/10_KubernetesPlugin/output/`
3. **Verify**: `_cluster/` subdirectory exists with cluster-scoped resources, namespace subdirectory has namespaced resources

#### Test Case 4 (Negative): Whiteout of cluster-scoped resources

1. Create a custom plugin stage that whiteouts a specific ClusterRole
2. Run transform
3. **Verify**: ClusterRole is written to `resources/` but commented out in `kustomization.yaml`

#### Test Case 5 (Negative): Duplicate cluster-scoped resource across stages

1. Verify that if the same ClusterRole appears in input twice, deduplication works correctly (`writer.go` `getResourceID` handles this)

### Step 1.2: Fix Any Bugs Found

Address issues discovered during manual testing. Likely areas based on code review:

- Patch target namespace handling in `writer.go` (currently passes `artifact.Target.Namespace` directly — should be fine for empty string since `PatchTarget.Namespace` uses `omitempty`)
- `kustomization.yaml` generation with mixed resources
- Inter-stage `_cluster/` directory preservation

### Step 1.3: Unit Tests for Transform

**File**: `internal/transform/writer_test.go` (new or extend existing)

Tests to add:

1. **`TestWriteStage_ClusterScopedResources`**: Write a stage with only cluster-scoped resources (ClusterRole, ClusterRoleBinding). Verify:
   - Resource files created with `clusterscoped` in filename
   - `kustomization.yaml` lists them correctly
   - No namespace in resource paths

2. **`TestWriteStage_MixedNamespacedAndClusterScoped`**: Write a stage with both Deployment (namespaced) and ClusterRole (cluster-scoped). Verify:
   - Both resource files created
   - `kustomization.yaml` lists both
   - Patch targets: namespaced has namespace, cluster-scoped omits it

3. **`TestWriteStage_ClusterScopedWhiteout`**: Cluster-scoped resource with whiteout. Verify:
   - Resource file written to disk
   - Commented out in `kustomization.yaml`

4. **`TestWriteStage_ClusterScopedWithPatch`**: Cluster-scoped resource with a JSONPatch. Verify:
   - Patch file created with correct filename (no namespace prefix)
   - Patch target in `kustomization.yaml` omits namespace

5. **`TestGetResourceID_ClusterScoped`**: Verify `getResourceID()` returns `Kind/name` for cluster-scoped.

**File**: `internal/transform/orchestrator_test.go` (extend existing)

6. **`TestRunMultiStage_ClusterScopedResources`**: Full orchestrator test with mixed resources flowing through stages. Verify inter-stage output directory structure includes `_cluster/`.

**Key functions to reuse**:
- `cranelib.DeriveTargetFromResource()` — `github.com/konveyor/crane-lib/transform/types.go:78`
- `file.GetResourceFilename()` — `internal/file/file_helper.go:234`
- `kustomize.GenerateKustomization()` — crane-lib `transform/kustomize/kustomize.go`
- `kustomize.GeneratePatchFilename()` — crane-lib `transform/kustomize/patch.go`

---

## Phase 2: Apply — `--skip-cluster-scoped` Flag, Tests, E2E

### Step 2.1: Add `--skip-cluster-scoped` Flag to `crane apply`

**Approach**: Post-build filtering — render everything via `kubectl kustomize`, then skip cluster-scoped resources when writing output files.

**File**: `cmd/apply/apply.go`
- Add `SkipClusterScoped bool` to `Flags` struct with mapstructure tag `"skip-cluster-scoped"`
- Add flag registration: `cmd.Flags().BoolVar(&o.SkipClusterScoped, "skip-cluster-scoped", false, "Exclude cluster-scoped resources from output (for non-admin migration scenarios)")`
- Pass to `KustomizeApplier`

**File**: `internal/apply/kustomize.go`
- Add `SkipClusterScoped bool` to `KustomizeApplier` struct
- In `splitMultiDocYAMLToFiles()`, after extracting namespace, add:
  ```go
  if namespace == "" && k.SkipClusterScoped {
      k.Log.Infof("Skipping cluster-scoped resource %s/%s (--skip-cluster-scoped)", kind, name)
      continue
  }
  ```
- Also filter cluster-scoped resources from `output.yaml` (the combined multi-doc file)

### Step 2.2: Unit Tests for Apply

**File**: `internal/apply/kustomize_test.go` (extend existing)

Tests to add:

1. **`TestSplitMultiDocYAML_SkipClusterScoped`**: Multi-doc YAML with mixed resources + `SkipClusterScoped=true`. Verify `_cluster/` directory not created, namespaced resources still written.

2. **`TestSplitMultiDocYAML_ClusterScopedIncluded`**: Same input with `SkipClusterScoped=false` (default). Verify `_cluster/` directory created with correct files.

3. **`TestSplitMultiDocYAML_OnlyClusterScoped`**: Edge case: only cluster-scoped resources + skip flag. Verify output directory exists but `resources/` is empty.

4. **`TestApplyMultiStage_SkipClusterScoped`**: Full apply flow with skip flag enabled. Verify `output.yaml` still contains all resources but split files exclude cluster-scoped.

### Step 2.3: E2E Tests

#### E2E Test 1: ClusterRole + ClusterRoleBinding Migration

**File**: `e2e-tests/tests/mta_cluster_scoped_rbac_test.go` (new)
**Label**: `Label("cluster-scoped", "tier1")`

1. Deploy nginx + ServiceAccount + ClusterRole + ClusterRoleBinding on src
2. Run full crane pipeline: export → transform → apply
3. Verify output contains both namespaced and cluster-scoped resources
4. Apply to target cluster (admin context)
5. Verify ClusterRole and ClusterRoleBinding exist on target
6. Verify ServiceAccount can perform the actions granted by ClusterRole
7. Cleanup: delete ClusterRole, ClusterRoleBinding on both clusters

#### E2E Test 2: CRD + Custom Resource Migration

**File**: `e2e-tests/tests/mta_cluster_scoped_crd_test.go` (new)
**Label**: `Label("cluster-scoped", "tier1")`

1. Create simple CRD (`widgets.example.com`) on src cluster
2. Create namespace with Widget CR instance
3. Run full crane pipeline
4. Verify CRD appears in output `_cluster/` directory
5. Apply CRD to target first, then apply namespaced resources
6. Verify Widget CR exists on target
7. Cleanup: delete CRD and CR on both clusters

#### E2E Test 3: --skip-cluster-scoped Flag

**File**: `e2e-tests/tests/mta_skip_cluster_scoped_test.go` (new)
**Label**: `Label("cluster-scoped", "tier1")`

1. Same setup as RBAC test
2. Run crane apply with `--skip-cluster-scoped`
3. Verify output does NOT contain ClusterRole/ClusterRoleBinding
4. Verify namespaced resources still present
5. Apply output to target (non-admin context) — should succeed without cluster-scoped resources

**Test data files**:
- `e2e-tests/testdata/clusterrole-test.yaml` — ClusterRole + ClusterRoleBinding fixtures
- `e2e-tests/testdata/test-crd.yaml` — Simple CRD definition

**Framework helpers to add** (if needed):
- `e2e-tests/framework/cluster_resources.go` — helper to create/cleanup cluster-scoped resources

---

## Verification Plan

### Phase 1 Verification

```bash
# Run unit tests for transform
go test -v -run "TestWriteStage_ClusterScoped\|TestWriteStage_Mixed\|TestGetResourceID" ./internal/transform/

# Run all transform tests to check for regressions
go test -v ./internal/transform/...

# Run full unit test suite
go test ./...
```

### Phase 2 Verification

```bash
# Run unit tests for apply
go test -v -run "TestSplitMultiDocYAML_SkipCluster\|TestSplitMultiDocYAML_ClusterScoped\|TestApplyMultiStage_Skip" ./internal/apply/

# Run all apply tests
go test -v ./internal/apply/...

# Run E2E tests (requires minikube clusters)
go install github.com/onsi/ginkgo/v2/ginkgo@v2.28.1
ginkgo run -v --label-filter="cluster-scoped" e2e-tests/tests -- \
  --crane-bin=./crane --source-context=src --target-context=tgt

# Build and verify binary
go build -o crane .
./crane apply --help  # Verify --skip-cluster-scoped flag appears
```

### Manual Smoke Test

```bash
# Full pipeline with cluster-scoped resources
kubectl --context=src create ns smoke-test
kubectl --context=src -n smoke-test create sa test-sa
kubectl --context=src create clusterrole smoke-reader --verb=get --resource=pods
kubectl --context=src create clusterrolebinding smoke-reader-binding \
  --clusterrole=smoke-reader --serviceaccount=smoke-test:test-sa

./crane export -n smoke-test
./crane transform
./crane apply
ls -la output/resources/_cluster/   # Should contain ClusterRole + ClusterRoleBinding

./crane apply --skip-cluster-scoped -o output-no-cluster
ls output-no-cluster/resources/     # Should NOT contain _cluster/

# Cleanup
kubectl --context=src delete clusterrolebinding smoke-reader-binding --ignore-not-found
kubectl --context=src delete clusterrole smoke-reader --ignore-not-found
kubectl --context=src delete ns smoke-test --ignore-not-found
```

---

## Summary of Files to Create/Modify

| Phase | File | Action |
|-------|------|--------|
| 1 | `internal/transform/writer_test.go` | Add cluster-scoped test cases |
| 1 | `internal/transform/orchestrator_test.go` | Add mixed resource multi-stage test |
| 2 | `cmd/apply/apply.go` | Add `--skip-cluster-scoped` flag |
| 2 | `internal/apply/kustomize.go` | Add post-build filtering logic |
| 2 | `internal/apply/kustomize_test.go` | Add skip-cluster-scoped tests |
| 2 | `e2e-tests/tests/mta_cluster_scoped_rbac_test.go` | New: RBAC migration E2E |
| 2 | `e2e-tests/tests/mta_cluster_scoped_crd_test.go` | New: CRD migration E2E |
| 2 | `e2e-tests/tests/mta_skip_cluster_scoped_test.go` | New: Skip flag E2E |
| 2 | `e2e-tests/testdata/clusterrole-test.yaml` | New: test fixtures |
| 2 | `e2e-tests/testdata/test-crd.yaml` | New: CRD fixture |
| 2 | `e2e-tests/framework/cluster_resources.go` | New: cluster-scoped helpers (if needed) |

## Acceptance Criteria Mapping

| Acceptance Criteria | Phase | Covered By |
|---------------------|-------|------------|
| ClusterRole, ClusterRoleBinding, CRD survive transform and appear in `output/resources/_cluster/` | 1 + 2 | Manual testing + unit tests + E2E |
| Namespaced resources remain under `output/resources/<namespace>/` | 1 + 2 | Unit tests + E2E |
| `--skip-cluster-scoped` flag omits cluster-scoped resources from output | 2 | Step 2.1 + unit tests |
| Patch target generation for cluster-scoped resources omits namespace | 1 | Unit tests (already works in code) |
| Recursive apply of output works for mixed namespaced + cluster-scoped cases | 2 | E2E tests |
| Unit tests: transform writer with cluster-scoped resources | 1 | Step 1.3 |
| Unit tests: apply split-output layout for cluster-scoped resources | 2 | Step 2.2 |
| Unit/integration test: mixed namespaced + cluster-scoped pipeline | 1 + 2 | Steps 1.3 + 2.2 |
| E2E test: cluster-scoped dependency migration works end to end | 2 | Step 2.3 |
