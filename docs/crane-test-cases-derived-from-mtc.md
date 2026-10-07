# Crane Test Cases — Derived from MTC Test Suite

Test cases for crane's migration pipeline (export → transform → apply → validate → transfer-pvc) derived from 220 MTC test cases. Focus: find bugs and gaps in crane.

**Coverage key:**
- **Covered** — existing e2e test covers this
- **Partial** — partially covered by an existing test
- **Not covered** — no existing test, gap

---

## 1. Export — Resource Discovery & Completeness

| # | Test Case | MTC Ref | Coverage | Crane E2E |
|---|-----------|---------|----------|-----------|
| E1 | Export Deployment with PVC — verify all resources captured | MTC-197, MTC-338 | Covered | mta_801, mta_806 |
| E2 | Export StatefulSet with volumeClaimTemplates — verify template-generated PVCs exported | MTC-196 | Covered | mta_805 |
| E3 | Export DaemonSet — verify exported correctly | MTC-196, MTC-79 | Covered | mta_805 |
| E4 | Export ReplicaSet (standalone, no owning Deployment) | MTC-196 | Covered | mta_805 |
| E5 | Export Job and CronJob — verify both captured | MTC-312, MTC-273 | Covered | mta_808 |
| E6 | Export CronJob with attached PVC — verify PVC reference preserved | MTC-131, MTC-314 | Covered | mta_813 |
| E7 | Export Pods with init containers — verify init containers in exported manifest | MTC-311 | Covered | mta_809 |
| E8 | Export ConfigMap — verify all data keys preserved | MTC-302 | Covered | mta_810 |
| E9 | Export Roles and RoleBindings — verify RBAC resources exported | MTC-177, MTC-102 | Covered | mta_812 |
| E10 | Export resources with custom labels and annotations — verify preserved | MTC-187, MTC-304 | Not covered | — |
| E11 | Export PV annotations — verify annotations preserved on exported PVC | MTC-372 | Not covered | — |
| E12 | Export namespace with DNS-invalid name (starts with number) | MTC-369 | Not covered | — |
| E13 | Export namespace with long name (>63 chars) | MTC-87 | Not covered | — |
| E14 | Export namespace with no PVCs — verify export succeeds | MTC-129 | Covered | mta_817, mta_851 |
| E15 | Export unattached PVCs (not referenced by any workload) | MTC-128, MTC-106 | Not covered | — |
| E16 | Export PVC in Terminating state — verify export handles gracefully | MTC-165 | Not covered | — |
| E17 | Export empty PVC (0 bytes data) — verify export succeeds | MTC-153 | Covered | mta_804 |
| E18 | Export namespace with ResourceQuota — verify quota object exported | MTC-306, MTC-269 | Not covered | — |
| E19 | Export namespace with LimitRange — verify LimitRange exported | MTC-341, MTC-340 | Not covered | — |
| E20 | Export multiple namespaces — run export per namespace, verify isolation | MTC-167, MTC-101 | Not covered | — |
| E21 | Export with label selector — verify only matching resources exported | MTC-187 | Not covered | — |
| E22 | Export CRD-backed custom resources — verify custom resources exported | MTC-139, MTC-235 | Covered | mta_855 |
| E23 | Export project with node selector — verify nodeSelector preserved | MTC-304 | Not covered | — |
| E24 | Export cluster-scoped resources (ClusterRole, ClusterRoleBinding) referenced by namespace workloads | MTC-177, MTC-370 | Covered | mta_852, mta_854 |

---

## 2. Transform — Plugin Correctness

| # | Test Case | MTC Ref | Coverage | Crane E2E |
|---|-----------|---------|----------|-----------|
| T1 | KubernetesPlugin strips server-managed metadata (uid, resourceVersion, status) | MTC-197 | Covered | mta_801 (implicit) |
| T2 | KubernetesPlugin whiteouts Endpoints and EndpointSlices | MTC-197 | Partial | mta_802 (ignored resources) |
| T3 | KubernetesPlugin whiteouts PVCs (default behavior) | MTC-197 | Covered | mta_801 (implicit) |
| T4 | KubernetesPlugin whiteouts OLM resources (Subscriptions, InstallPlans, CSVs) | MTC-350 | Covered | olm_whiteout_base, olm_whiteout_auditability |
| T5 | KubernetesPlugin whiteouts resources with ownerReferences | MTC-197 | Covered | mta_801 (implicit) |
| T6 | KubernetesPlugin does NOT whiteout standalone ReplicaSets | MTC-196 | Partial | mta_805 (sets, but not explicitly standalone RS) |
| T7 | KubernetesPlugin `pvc-rename-map` renames PVC reference in Deployment | MTC-342, MTC-83 | Not covered | — |
| T8 | KubernetesPlugin `pvc-rename-map` renames PVC reference in StatefulSet volumeClaimTemplates | MTC-342 | Not covered | — |
| T9 | KubernetesPlugin `pvc-rename-map` renames PVC reference in CronJob | MTC-131 | Not covered | — |
| T10 | KubernetesPlugin `pvc-rename-map` with multiple mappings | MTC-167 | Not covered | — |
| T11 | OpenShiftPlugin strips SCC-injected securityContext from Deployments | MTC-373, MTC-163 | Partial | mta_801 (OCP only, implicit) |
| T12 | OpenShiftPlugin strips SCC-injected securityContext from standalone Pods | MTC-196 | Not covered | — |
| T13 | OpenShiftPlugin strips SCC-injected securityContext from StatefulSets, DaemonSets, Jobs, CronJobs | MTC-196 | Not covered | — |
| T14 | OpenShiftPlugin strips default pull secrets | MTC-204 | Not covered | — |
| T15 | OpenShiftPlugin strips default RBAC (deployers, builders, image-pullers) | MTC-177 | Partial | mta_812 (role migration, not explicit RBAC stripping) |
| T16 | OpenShiftPlugin strips OVN annotations from standalone Pods | MTC-311 | Not covered | — |
| T17 | OpenShiftPlugin `registry-replacement` flag maps image registries | MTC-367 | Not covered | — |
| T18 | Transform with custom stage (user-provided kustomization) | MTC-350 | Covered | mta_827 |
| T19 | Transform with instructions file specifying stage order | MTC-350 | Covered | mta_828 |
| T20 | Transform idempotency — running twice produces same output | MTC-197 | Not covered | — |
| T21 | Transform with `--overwrite` flag replaces existing transform dir | MTC-197 | Partial | mta_830 (force reconcile) |
| T22 | Transform handles excluded/ignored resources | MTC-127, MTC-161 | Covered | mta_802 |
| T23 | SA token Secrets (kubernetes.io/service-account-token) should be whiteout-ed | MTC-204, MTC-322 | Not covered | — (known bug #484) |
| T24 | Transform Deployment with supplemental groups — verify preserved or stripped | MTC-352, MTC-96 | Not covered | — |

---

## 3. Apply — Manifest Rendering

| # | Test Case | MTC Ref | Coverage | Crane E2E |
|---|-----------|---------|----------|-----------|
| A1 | Apply renders valid YAML from transform output | MTC-197 | Covered | mta_801 (implicit) |
| A2 | Apply renders multi-resource output.yaml | MTC-338 | Covered | mta_801 (implicit) |
| A3 | Apply with multi-stage transform — final stage output used | MTC-350 | Covered | mta_827, mta_828 |
| A4 | Apply with `--skip-cluster-scoped` | MTC-177 | Covered | mta_853 |
| A5 | Apply output can be successfully `kubectl apply`-ed to target cluster | MTC-338 | Covered | mta_801, mta_817 |
| A6 | Apply output for namespace with ResourceQuota | MTC-306 | Not covered | — |

---

## 4. Validate — API Compatibility

| # | Test Case | MTC Ref | Coverage | Crane E2E |
|---|-----------|---------|----------|-----------|
| V1 | Validate detects deprecated API version | MTC-280, MTC-281 | Covered | mta_844, mta_845, mta_846 |
| V2 | Validate suggests alternative API version | MTC-280 | Covered | mta_829, mta_832 |
| V3 | Validate reports OK for all compatible resources | MTC-281 | Covered | mta_831, mta_833 |
| V4 | Validate detects CRD-backed resource not available on target | MTC-139 | Covered | mta_849 |
| V5 | Validate exit code 0 when compatible, non-zero when incompatible | MTC-280 | Covered | mta_833, mta_844 |
| V6 | Validate in offline mode with captured API surface | MTC-351, MTC-353 | Covered | mta_831, mta_845, mta_860, mta_861 |

---

## 5. Transfer-PVC — Data Migration

| # | Test Case | MTC Ref | Coverage | Crane E2E |
|---|-----------|---------|----------|-----------|
| P1 | Transfer PVC with basic file data — verify data intact | MTC-197 | Covered | mta_806 |
| P2 | Transfer PVC with database data (MySQL) — verify data intact | MTC-234, MTC-132 | Covered | mta_807 |
| P3 | Transfer PVC with symbolic links — verify symlinks preserved | MTC-377 | Not covered | — |
| P4 | Transfer PVC with verify_copy (checksum verification) | MTC-79, MTC-84 | Not covered | — |
| P5 | Transfer PVC with long name (>63 chars) — verify truncation | MTC-147, MTC-87 | Not covered | — |
| P6 | Transfer empty PVC — verify no error | MTC-153 | Covered | mta_804 |
| P7 | Transfer PVC with large data (>1GB) — verify completion | MTC-234 | Not covered | — |
| P8 | Transfer PVC with dest-storage-class — verify target SC | MTC-83, MTC-342 | Not covered | — |
| P9 | Transfer PVC with namespace mapping (source:destination) | MTC-172, MTC-90 | Not covered | — |
| P10 | Transfer PVC with name mapping (source:destination) | MTC-172 | Not covered | — |
| P11 | Transfer PVC using Route endpoint (OCP) | MTC-338 | Not covered | — (manual OCP tests only) |
| P12 | Transfer PVC using nginx-ingress endpoint (K8s) | MTC-338 | Covered | mta_806, mta_807 |
| P13 | Transfer PVC as non-admin (namespace-admin RBAC) | MTC-163 | Covered | mta_813 |
| P14 | Transfer PVC with OCP restricted-v2 SCC — verify UID detection | MTC-352, MTC-96 | Not covered | — (manual OCP tests only) |
| P15 | Transfer PVC with supplemental groups | MTC-352 | Not covered | — |
| P16 | Transfer multiple PVCs for same namespace | MTC-167, MTC-101 | Not covered | — |
| P17 | Transfer PVC same-cluster (intra-cluster SC conversion) | MTC-342, MTC-347 | Not covered | — (new feature) |
| P18 | Transfer PVC with different SC source vs target | MTC-318, MTC-315 | Not covered | — |
| P19 | Transfer PVC progress reporting — verify stats output | MTC-107 | Not covered | — |
| P20 | Transfer PVC with rsync exit code 23 — verify warning not error | MTC-229, MTC-236 | Not covered | — |
| P21 | Transfer PVC when source PVC is RWO — verify rsync on same node | MTC-123 | Partial | mta_806 (implicit, not explicit node check) |
| P22 | Transfer PVC with LimitRange on target — verify pods respect limits | MTC-341, MTC-340 | Not covered | — |
| P23 | Transfer PVC with ResourceQuota on target — verify pods schedulable | MTC-269, MTC-271 | Not covered | — |

---

## 6. End-to-End Pipeline — Full Migration Workflow

| # | Test Case | MTC Ref | Coverage | Crane E2E |
|---|-----------|---------|----------|-----------|
| M1 | Migrate Deployment + PVC (basic file app) | MTC-197, MTC-338 | Covered | mta_801, mta_806 |
| M2 | Migrate Deployment + PVC (MySQL) | MTC-234, MTC-132 | Covered | mta_807 |
| M3 | Migrate Deployment + PVC (MongoDB) | MTC-272 | Covered | mta_811 |
| M4 | Migrate Deployment + PVC (PostgreSQL/Django) | MTC-305, MTC-195 | Not covered | — |
| M5 | Migrate Deployment + PVC (Redis) | MTC-303 | Not covered | — |
| M6 | Migrate StatefulSet with volumeClaimTemplates (2+ replicas) | MTC-196 | Partial | mta_805 (export/transform only, no PVC transfer) |
| M7 | Migrate DaemonSet | MTC-196, MTC-79 | Partial | mta_805 (export/transform only) |
| M8 | Migrate Job | MTC-312 | Not covered | — |
| M9 | Migrate CronJob with PVC — verify quiesce + transfer + fires on target | MTC-273, MTC-131 | Covered | mta_808, mta_813 |
| M10 | Migrate ConfigMap + Secret — verify data preserved | MTC-302 | Covered | mta_810 |
| M11 | Migrate project with Roles and RoleBindings | MTC-177, MTC-102 | Covered | mta_812 |
| M12 | Migrate project with labels and node selectors | MTC-304 | Not covered | — |
| M13 | Migrate to a different target namespace name | MTC-90, MTC-172 | Not covered | — |
| M14 | Migrate multiple namespaces (10+) sequentially | MTC-88, MTC-167 | Not covered | — |
| M15 | Migrate namespace with ResourceQuota | MTC-306, MTC-269 | Not covered | — |
| M16 | Migrate Pods with init containers | MTC-311 | Covered | mta_809 |
| M17 | Migrate app from OCP to OCP | MTC-235, MTC-338 | Not covered | — (manual OCP tests only) |
| M18 | Migrate app from OCP to vanilla K8s | MTC-280 | Not covered | — |
| M19 | Migrate app from K8s to OCP | MTC-197 | Not covered | — |
| M20 | Full pipeline as non-admin user | MTC-163 | Covered | mta_813, mta_852, mta_853 |

---

## 7. Edge Cases & Error Handling

| # | Test Case | MTC Ref | Coverage | Crane E2E |
|---|-----------|---------|----------|-----------|
| X1 | Export from namespace that doesn't exist — verify clear error | MTC-369 | Not covered | — |
| X2 | Transfer PVC that doesn't exist — verify error message | MTC-165 | Not covered | — |
| X3 | Transfer PVC with target SC that doesn't exist — verify error | MTC-83 | Not covered | — |
| X4 | Transform with invalid optional-flags JSON — verify parse error | MTC-111 | Not covered | — |
| X5 | Export with invalid kubeconfig context — verify error | MTC-278 | Not covered | — |
| X6 | Transfer PVC with insufficient RBAC — verify Forbidden handled | MTC-163 | Not covered | — |
| X7 | Long PVC name causing Route hostname >63 chars — verify truncation | MTC-147, MTC-87 | Not covered | — |
| X8 | Transfer PVC re-run (dest PVC already exists) — verify AlreadyExists handled | MTC-122 | Not covered | — |
| X9 | Export namespace with >100 resources — verify no timeout/memory issues | MTC-154 | Not covered | — |
| X10 | PVC with mixed file ownership (multiple UIDs) — verify rsync handles | MTC-96 | Not covered | — |
| X11 | PVC data validation after transfer — md5sum comparison | MTC-199 | Covered | mta_807 (MySQL checksum) |
| X12 | Export/transform/apply pipeline with no PVCs — verify clean pipeline | MTC-129 | Covered | mta_817, mta_851 |

---

## 8. StorageClass Conversion (Intra-cluster)

| # | Test Case | MTC Ref | Coverage | Crane E2E |
|---|-----------|---------|----------|-----------|
| S1 | Same-cluster transfer-pvc (standard → standard-v2 on minikube) | MTC-342 | Not covered | — (new feature) |
| S2 | Same-cluster transfer-pvc (gp2-csi → gp3-csi on OCP) | MTC-347, MTC-348 | Not covered | — (new feature) |
| S3 | SC conversion + pvc-rename-map in transform — full pipeline | MTC-342, MTC-83 | Not covered | — (new feature) |
| S4 | SC conversion with StatefulSet — transfer PVCs + pvc-rename-map | MTC-196, MTC-342 | Not covered | — (new feature) |
| S5 | SC conversion with multiple PVCs — transfer each, single transform pass | MTC-349, MTC-345 | Not covered | — (new feature) |
| S6 | SC conversion as non-admin | MTC-163, MTC-342 | Not covered | — (new feature) |

---

## Coverage Summary

| Category | Total | Covered | Partial | Not Covered |
|----------|-------|---------|---------|-------------|
| Export | 24 | 11 | 0 | 13 |
| Transform | 24 | 7 | 4 | 13 |
| Apply | 6 | 5 | 0 | 1 |
| Validate | 6 | 6 | 0 | 0 |
| Transfer-PVC | 23 | 4 | 2 | 17 |
| End-to-End | 20 | 8 | 2 | 10 |
| Edge Cases | 12 | 2 | 0 | 10 |
| SC Conversion | 6 | 0 | 0 | 6 |
| **Total** | **121** | **43 (36%)** | **8 (7%)** | **70 (58%)** |

### Biggest Gaps

1. **Transfer-PVC** — 17/23 not covered. Most PVC transfer scenarios have no e2e test.
2. **Edge Cases** — 10/12 not covered. Error handling and boundary conditions untested.
3. **Export edge cases** — Labels, annotations, ResourceQuota, LimitRange, long names, unattached PVCs.
4. **Transform plugins** — pvc-rename-map, OpenShift plugin SCC stripping, OVN annotations, registry replacement all untested in e2e.
5. **SC Conversion** — entirely new, 0/6 covered.
6. **Cross-platform** — OCP-to-OCP, OCP-to-K8s, K8s-to-OCP migration paths have no automated tests.

---

## Not Applicable to Crane (Excluded)

| Category | Count | Reason |
|----------|-------|--------|
| UI-only tests | 23 | Crane has no UI |
| MTC operator/controller tests | 70 | Crane is CLI-only, no operator |
| Velero/OADP specific | ~15 | Crane uses rsync, not Velero |
| Image migration (ImageStreams) | 15 | Crane exports manifests but does not transfer image data |
| MigPlan/MigCluster/MigStorage CRDs | ~10 | Crane has no CRDs |
| Hooks (pre/post migration) | 5 | Crane has no hook mechanism |
| Rollback | 5 | Crane is non-destructive — old resources preserved |
| Debug/Analytics UI | 5 | No UI |
