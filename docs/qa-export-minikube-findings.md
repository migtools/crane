# `crane export` — Minikube QA findings

This document records results from the rigorous QA matrix (fixtures under [`hack/qa-export-minikube/`](../hack/qa-export-minikube/)). For **per-case** runs (no single loop script), use [`hack/qa-export-minikube/tc/`](../hack/qa-export-minikube/tc/README.md): execute scripts in [`ORDER.txt`](../hack/qa-export-minikube/tc/ORDER.txt) order; each appends a row to `hack/qa-export-minikube/checklist-results.md` (gitignored) unless `RESULTS` is set.

The older driver `./hack/qa-export-minikube/run-matrix.sh` still writes `hack/qa-export-minikube/last-run-report.md` (gitignored).

## Preflight (example run)

| Item | Example |
|------|---------|
| Minikube | v1.37.0 |
| kubectl context | minikube |
| crane | v0.0.6 (built from repo) |
| Kubernetes | 1.33.x (varies with Minikube driver) |

## Traceability summary

Automated rows exercise IDs **A1–A10**, **B1**, **B12**, **B13**, **C1–C3**, **D1–D6** (D5 N/A), **E1–E5** (E4 N/A), **F1–F4**, **G1–G3**, **H** (observational).

**Workload coverage B2–B11** is satisfied by the shared fixture in `hack/qa-export-minikube/workloads.yaml` (StatefulSet, DaemonSet, Job, CronJob, ConfigMap, Secret, ServiceAccount, Role/RoleBinding, NetworkPolicy, Ingress, PDB, HPA, LimitRange, ResourceQuota, ReplicationController). When **B1** passes with many resource files, those kinds are present in the export output; spot-check filenames under `resources/crane-qa/` (e.g. `StatefulSet_*`, `DaemonSet_*`, `Job_*`).

| ID | Result (typical) | Notes |
|----|------------------|-------|
| A1–A2, A4, A7, A9–A10, B1, B12–B13, D1-D2, G1–G2 | Pass | |
| A3, A5, F4 | Pass (non-zero exit) | Invalid `-l`, `--as-extras` without impersonation, read-only export parent |
| A8 | **Fail (bug)** | See PB-001 |
| C1–C3, E1–E3, F2–F3, G3 | Pass/obs | Observe file counts and logs |
| D5, E4, H | N/A | OpenShift SCC; subresource-only CRD; optional API groups |

## Observed behavior — label selectors (doc-behavior)

`--label-selector` is passed to every `List` call. Many namespaced types return **no items** when no object matches, so **few or zero** manifest files are written (e.g. **C2** ~0 files). Types that still list unrelated objects are not expected—Kubernetes applies the selector server-side per resource.

- **C1** (`app=a`): Only workloads whose **labels** match are listed per GVR; dependent objects (e.g. ReplicaSet) often match via pod template labels—file count stays small vs full export.
- **C2** (`nonexistent=key`): Near-empty export (~0 files) when nothing matches.
- **C3** (`app.kubernetes.io/name=crane-qa`): Standard label key/value; namespaced objects (including Widgets) match. **`tc-c03.sh`** passes `--crd-skip-group craneqatest.example.com` so the run completes: without it, related CRD export can try to write under `_cluster` when that directory was never created (see **PB-005** and [`docs/bugs/export-crd-missing-cluster-dir.md`](../bugs/export-crd-missing-cluster-dir.md)). **E\*** cases still cover CRD export without that skip.
- **A04** (third selector, `env`): Existence selector; objects with label key `env` match (e.g. ConfigMap `crane-qa-config`).

## Observed behavior — exit codes vs `failures/`

- **`Run()`** returns an aggregate of **write** errors only; **list/GET** errors are persisted under `failures/<namespace>/` but often still produce **exit 0** (**F1**, **F2**, **F3** demonstrate many failure files with exit 0 when writes succeed).
- This matches code in [`cmd/export/export.go`](../cmd/export/export.go) (`resourceErrs` not merged into the returned error).

## Probable bugs

| Bug-ID | Test-ID | Severity | Summary |
|--------|---------|----------|---------|
| PB-001 | A8 | High | `crane export -n <nonexistent-namespace>` completes with **exit 0**, lists cluster-scoped RBAC cluster-wide, and writes under `resources/<name>/` without validating that the namespace exists. Repro: `crane export -e /tmp/t -n nonexistent-ns-99999`; observe exit=0. **Expected:** fail fast with clear error and non-zero exit. |
| PB-002 | F1, F2, F3 | Medium | Partial list/CRD GET failures recorded in `failures/` but command exits **0**—misleading for CI and operators. **Expected (product decision):** optional strict mode or non-zero exit when any list error occurs. |
| PB-003 | E3 | Low | With `--crd-include-group apps`, multiple `customresourcedefinitions` GET attempts and log lines for built-in plural names may produce noise and `failures/` entries (NotFound). **Expected:** quieter handling or documented behavior. |
| PB-004 | — | Nit | Cobra occasionally prints **Global Flags** help to stderr when an invalid flag combination is used; worth tracing if any wrapper invokes `crane` with wrong argument order. |
| PB-005 | C3 | Medium | With `-l` such that **only** namespaced lists return data (no ClusterRole/Binding under the same selector) but **Widget** (or other CR) types still match, `collectRelatedCRDs` adds a CRD row and `writeResources` can fail with **missing `_cluster` parent** (non-zero exit). Documented in [`docs/bugs/export-crd-missing-cluster-dir.md`](../bugs/export-crd-missing-cluster-dir.md). QA workaround: `--crd-skip-group craneqatest.example.com` in `tc-c03.sh`. |

## Fixtures layout

| File | Purpose |
|------|---------|
| `kustomization.yaml` | Ordered apply: CRD before Widget CRs |
| `namespace.yaml`, `namespace-empty.yaml` | `crane-qa`, `crane-qa-empty` |
| `workloads.yaml` | Core workloads + labels for C* |
| `rbac-namespaced.yaml` | B7 |
| `cluster-rbac.yaml` | D1, D3, D4, D6 |
| `cluster-rbac-d2-user-only.yaml` | D2 (optional apply) |
| `crd-widget.yaml`, `widgets-cr.yaml` | E* |
| `rbac-limited.yaml` | F1–F3 ServiceAccounts + Roles |
| `flags-sample.yaml` | A10 |
| `run-matrix.sh` | Build + apply + matrix |

## Cleanup

See [`hack/qa-export-minikube/README.md`](../hack/qa-export-minikube/README.md).
