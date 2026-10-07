# MTC API-skew experiments (Crane docs)

## Reality check: 4.14 vs 4.17 (same nightly pair as QE)

- **Namespaced** `oc api-resources` rows are a **subset** on the source vs the target in our comparison: there was **no** built-in namespaced `(apiVersion, resource)` that existed **only** on 4.14. The target often had **more** types (e.g. `k8s.ovn.org` egress objects).
- **Cluster-scoped** types present **only** on the OpenShift SDN source included `network.openshift.io` **ClusterNetwork / HostSubnet / NetNamespace**, and **flowcontrol** `v1beta3` rows—not suitable for a safe namespace-only “app” test.
- So a **reliable** skew test uses a **CRD installed only on the source** plus normal **Deployment/Service** as a control.

## `mtc-gvk-api-skew-sample.yaml` (EgressNetworkPolicy)

Earlier we assumed `network.openshift.io/v1` was missing on 4.17; on the tested **4.17 nightly**, **`EgressNetworkPolicy` still exists** and migrates. Treat that sample as **not** demonstrating missing API on target for that cluster pair.

## `mtc-skew-v2-source.yaml` + `mtc-skew-v2-migration-host.yaml`

1. Apply **`mtc-skew-v2-source.yaml`** on the **source** only. If the `SkewWidget` object fails to apply, wait for the CRD to be **Established** then re-apply the CR manifest (see comments in the YAML).
2. Confirm on the **target**: `oc get crd skewwidgets.skew.crane.test` → **NotFound**.
3. Apply **`mtc-skew-v2-migration-host.yaml`** on the **host** (adjust `MigCluster` / `MigStorage` names if your install differs).
4. Observe **`MigMigration`** phase and **`Restore`** (`warnings` count); on the target namespace check whether **`SkewWidget`** / CRD appeared (Velero often restores CRDs for migrated CRs).

### Observed run (automated, 2026-04-01)

- **`MigMigration` `skew-mig-1`:** `Completed`, itinerary `Final`.
- **Target namespace `mtc-skew-v2`:** `Deployment` + `Service` **Running**; **`skewwidgets.skew.crane.test` CRD still NotFound**; **`SkewWidget` resource type unavailable** (CR not present).
- **`Restore`:** `Completed`, **5 warnings**, **no errors** in status.
- **`MigPlan` `skew-test-plan`:** `status.incompatible` **empty**; **no `GVKsIncompatible`** condition (mig-controller’s compare path did not surface this custom group as incompatible in that run).

Interpretation: **MTC reported success** while **custom resources did not land** because the **CRD was not restored** (or not applicable on target) in this configuration—worth treating as a **partial functional success** and inspecting Velero logs for the five warnings.
