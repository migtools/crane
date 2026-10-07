# OLM Whiteout Test Plan

**Issue:** [migtools/crane#214](https://github.com/migtools/crane/issues/214)
**Date:** 2026-04-30

## Background

Crane-lib's `KubernetesTransformPlugin` whiteouts 6 OLM resource kinds during `transform`:

- `Subscription` (operators.coreos.com/v1alpha1)
- `CatalogSource` (operators.coreos.com/v1alpha1)
- `ClusterServiceVersion` (operators.coreos.com/v1alpha1)
- `InstallPlan` (operators.coreos.com/v1alpha1)
- `OperatorGroup` (operators.coreos.com/v1)
- `OperatorCondition` (operators.coreos.com)

Whiteout means: resources are exported correctly, written to `resources/` in transform output for auditability, but **excluded** from `kustomization.yaml` active resources so `kubectl kustomize` / `crane apply` never renders them.

## Existing Coverage


| Test                                                  | Level           | What it covers                                                                                             |
| ----------------------------------------------------- | --------------- | ---------------------------------------------------------------------------------------------------------- |
| `olm_whiteout_test.go` (baseline)                     | E2E             | Deploys real OLM operator, runs full pipeline, asserts no OLM kinds in apply output or on target cluster   |
| `mtc_127_ignored_resources_test.go`                   | E2E             | Tests Endpoints + Subscription whiteout with manually created resources (no real OLM)                      |
| `orchestrator_test.go` (`TestWhiteoutPropagation`)    | Unit            | Tests KustomizeWriter correctly includes whiteout resource files but excludes them from kustomization.yaml |
| `orchestrator_test.go` (`TestWhiteoutKustomizeBuild`) | Unit            | Tests `kubectl kustomize` excludes whiteout resources from rendered output                                 |
| crane-lib `kubernetes_test.go`                        | Unit (upstream) | Tests `getWhiteOuts()` returns true for each OLM GK individually                                           |


## Gap Analysis

The existing baseline test covers the "happy path" with a real OLM deployment. What's missing:

1. **OLM resources coexisting with app workloads** - The baseline test uses `olm-baseline` which is mostly just OLM resources. Real migrations have Deployments, Services, ConfigMaps alongside OLM resources. We need to verify the app resources survive while OLM is whiteout-ed.
2. **Transform-stage auditability** - No test verifies that whiteout resources appear as commented-out entries in `kustomization.yaml` or that the resource files themselves exist in `resources/`. This is important for auditability.
3. **Multiple OLM operators** - The baseline deploys a single operator. Real clusters often have multiple operators in the same namespace. Need to verify all OLM resources from multiple operators are whiteout-ed.
4. **Manually-created OLM resources** - Not all OLM resources are controller-managed. Some users create standalone Subscriptions or CatalogSources. These should still be whiteout-ed. (MTC-127 partially covers this with a Subscription-only test.)
5. **OLM resources with ownerReferences** - InstallPlans and CSVs are often owned by other OLM resources. The whiteout should fire based on the GK match (from `gksToWhiteout`), **not** just the ownerReference logic. Need to verify they're whiteout-ed for the right reason.
6. `**--optional-flags` interaction** - `disable-whiteout-owned` and `extra-whiteouts` / `include-only` flags interact with OLM whiteout logic. No test covers these interactions.

## Proposed Test Cases

### Test 1: OLM whiteout with application workloads (E2E, tier0)

**Label:** `[]`
**Goal:** Verify that when a namespace has both a running operator (OLM resources) and application workloads (Deployment, Service, ConfigMap), the application resources survive the pipeline while all OLM resources are whiteout-ed.

**Steps:**

1. Deploy an app that includes a Deployment + Service + ConfigMap alongside an OperatorGroup + CatalogSource + Subscription
2. Wait for OLM to create InstallPlan + CSV
3. Run full crane pipeline (export -> transform -> apply)
4. Assert: no OLM kinds in output (reuse `assertNoOLMWhiteoutKindsInOutput`)
5. Assert: application resources (Deployment, Service, ConfigMap) ARE present in output
6. Apply to target cluster
7. Assert: app resources exist on target, OLM resources do not

**Why it matters:** The baseline test doesn't verify that non-OLM resources coexist properly. A bug that whiteouts too aggressively (e.g., all resources in a namespace with OLM) would only be caught by this test.

### Test 2: Transform-stage auditability of OLM whiteouts (E2E, tier1)

**Label:** `[]`
**Goal:** Verify that whiteout-ed OLM resources are auditable in the transform stage output.

**Steps:**

1. Deploy OLM operator (or use manually-created OLM resources for speed)
2. Run crane export + transform (no need for apply)
3. Walk the transform stage directory:
  - Assert: each OLM resource has a file in `resources/` subdirectory
  - Assert: `kustomization.yaml` has whiteout comment lines (starting with `# - resources/`) for each OLM resource
  - Assert: OLM resource files are NOT in the active `resources:` list in `kustomization.yaml`
4. Run crane apply
5. Assert: OLM resources are absent from rendered output

**Why it matters:** Auditability is a core crane design principle. Users need to see what was whiteout-ed and why. This test validates the transform-stage output structure.

### Test 3: Multiple OLM operators in same namespace (E2E, tier1)

**Label:** `[]`
**Goal:** Verify whiteout works when multiple operators are installed in the same namespace, producing multiple Subscriptions, CSVs, InstallPlans.

**Steps:**

1. Deploy two separate operators in the same namespace (two Subscriptions, two CatalogSources, shared OperatorGroup)
2. Wait for both to reconcile (two CSVs, two InstallPlans)
3. Run full crane pipeline
4. Assert: all OLM resources from both operators are absent from output
5. Assert: no partial whiteout (e.g., one operator's CSV leaked through)

**Why it matters:** The whiteout logic matches on GroupKind, not specific names. But the export/transform flow could have ordering or deduplication bugs that surface only with multiple resources of the same kind.

### Test 4: Manually-created standalone OLM resources (E2E, tier0)

**Label:** `[MTC-214-MANUAL]`
**Goal:** Verify that OLM resources created manually (not by OLM controller) are still whiteout-ed, covering all 6 kinds explicitly.

**Steps:**

1. Deploy a namespace with manually-created YAML for all 6 OLM resource kinds:
  - Subscription, CatalogSource, ClusterServiceVersion, InstallPlan, OperatorGroup, OperatorCondition
2. No need to wait for OLM reconciliation (these are standalone)
3. Run full crane pipeline
4. Assert: none of the 6 kinds appear in output
5. Apply to target, assert none exist on target

**Why it matters:** This is a fast, deterministic test (no OLM controller dependency) that covers OperatorCondition (the baseline test doesn't explicitly create one). It ensures the whiteout fires on GK match alone, regardless of whether resources have ownerReferences or controller-managed metadata.

**Note:** Requires the OLM CRDs to be installed on the cluster (but not the OLM controller itself). If CRDs are not present, skip the test.

### Test 5: OLM whiteout with `disable-whiteout-owned` flag (Unit, extends orchestrator_test.go)

**Label:** `[MTC-214-FLAG]`
**Goal:** Verify that `disable-whiteout-owned=true` does NOT re-enable OLM resources. OLM resources are in `gksToWhiteout`, so they should be whiteout-ed regardless of the `DisableWhiteoutOwned` flag.

**Steps (unit test):**

1. Create unstructured objects for each OLM kind (Subscription, CSV, InstallPlan, CatalogSource, OperatorGroup, OperatorCondition)
2. Instantiate `KubernetesTransformPlugin` with `DisableWhiteoutOwned: true`
3. Call `Run()` for each resource
4. Assert: `IsWhiteOut == true` for all 6 kinds

**Why it matters:** `DisableWhiteoutOwned` only affects the ownerReference check, but a user might expect it to disable ALL whiteouts. This test confirms OLM GK-based whiteouts are unconditional.

### Test 6: `include-only` flag overrides OLM whiteout (Unit, extends orchestrator_test.go)

**Label:** `[MTC-214-INCLUDE]`
**Goal:** Verify that `include-only=Subscription.operators.coreos.com` causes Subscriptions to NOT be whiteout-ed (include-only takes precedence over `gksToWhiteout`).

**Steps (unit test):**

1. Create a Subscription unstructured object
2. Instantiate `KubernetesTransformPlugin` with `IncludeOnly: [{Group: "operators.coreos.com", Kind: "Subscription"}]`
3. Call `Run()` for the Subscription
4. Assert: `IsWhiteOut == false` (include-only overrides the default whiteout list)
5. Call `Run()` for a ConfigMap (not in include-only list)
6. Assert: `IsWhiteOut == true` (everything not in include-only is whiteout-ed)

**Why it matters:** `include-only` is the escape hatch for users who DO want to migrate OLM resources. This test verifies the override works.

## Implementation Priority


| Priority | Test                                 | Effort     | Cluster Required             |
| -------- | ------------------------------------ | ---------- | ---------------------------- |
| 1        | Test 4 (manual standalone)           | Low        | CRDs only, no OLM controller |
| 2        | Test 2 (auditability)                | Low-Medium | CRDs only (or OLM)           |
| 3        | Test 1 (app + OLM coexistence)       | Medium     | OLM controller               |
| 4        | Test 5 (disable-whiteout-owned flag) | Low        | None (unit test)             |
| 5        | Test 6 (include-only override)       | Low        | None (unit test)             |
| 6        | Test 3 (multiple operators)          | High       | OLM controller + 2 operators |


Tests 4, 5, and 6 are fast to implement and don't require a full OLM installation. Test 2 can work with manually-created resources. Tests 1 and 3 require OLM to be running.

## Notes

- Tests 5 and 6 are unit tests against crane-lib's `KubernetesTransformPlugin`. They belong in crane-lib, but since crane-lib is a dependency, we can write integration-level tests in crane that exercise the plugin through the full `Orchestrator.transformResources()` path.
- All E2E tests should check `OLMAPIAvailable()` and skip gracefully if OLM CRDs are not present.
- The `assertNoOLMWhiteoutKindsInOutput()` helper from the baseline test should be reused across all E2E tests.

