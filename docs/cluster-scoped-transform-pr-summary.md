# PR: Add tests for cluster-scoped resources in transform

Phase 1 of [issue #189](https://github.com/migtools/crane/issues/189). No production code changes — the existing transform already handles cluster-scoped resources correctly. This PR adds tests to prove and lock down that behavior.

## Manual Testing on Minikube (all PASS)

| Scenario | What was tested |
|----------|----------------|
| **RBAC** | nginx + ServiceAccount + ClusterRole + ClusterRoleBinding through full export → transform → apply. Verified `_cluster/` layout, patch targets omit namespace, `kubectl kustomize` succeeds, recursive apply works. |
| **CRD** | Custom CRD (`widgets.example.com`) + Widget CR instance. Verified CRD flows to `_cluster/`, CR stays under namespace directory. |
| **Multi-stage** | Two stages (`10_KubernetesPlugin` → `20_CustomStage`). Verified `_cluster/` separation preserved across stage boundaries — stage 2 input correctly loads from stage 1 output, content survives both stages. |

## Unit Tests Added (7 tests)

| File | Test | Verifies |
|------|------|----------|
| `writer_test.go` | `TestGetResourceID_ClusterScoped` | Returns `Kind/name` (no namespace) for cluster-scoped resources |
| `writer_integration_test.go` | `TestWriteStage_ClusterScopedResources` | Only cluster-scoped resources: filenames use `clusterscoped`, kustomization omits `namespace:` |
| | `TestWriteStage_MixedNamespacedAndClusterScoped` | Mixed resources: patch filenames and targets differ correctly |
| | `TestWriteStage_ClusterScopedWhiteout` | Whiteout: file on disk, commented out in kustomization.yaml |
| | `TestWriteStage_ClusterScopedWithPatch` | Patch file named without namespace prefix, target omits namespace |
| | `TestWriteStage_KustomizeBuildWithMixedResources` | `kubectl kustomize` succeeds on mixed namespaced + cluster-scoped stage |
| `orchestrator_test.go` | `TestMultiStage_ClusterScopedResources` | 2-stage pipeline: `_cluster/` separation preserved at every stage boundary, content intact |

## Files Changed

| File | Lines |
|------|-------|
| `internal/transform/writer_test.go` | +68 |
| `internal/transform/writer_integration_test.go` | +543 |
| `internal/transform/orchestrator_test.go` | +231 |
