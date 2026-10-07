# Workload validation test results (against `crane-validate-before-apply.md`)



---

## Numbered validation steps (reference)


| #     | What this step does                                                                                                                    | Command / action                                                             |
| ----- | -------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| **1** | Client-side dry-run: parse YAML and validate against the OpenAPI schema bundled with kubectl (no cluster mutation).                    | `kubectl apply --dry-run=client -f <dir> --recursive`                        |
| **2** | Server-side dry-run: send objects to the API server for validation against live APIs, CRDs, and admission (no persist).                | `kubectl apply --dry-run=server -f <dir> --recursive`                        |
| **3** | Permission check: confirm the current user (or `--as` identity) may **create** each resource type (plural name; `-n` when namespaced). | `kubectl auth can-i create <plural> [-n <ns>]` per manifest (see doc Step 3) |
| **4** | Dependency check: confirm referenced ConfigMaps, Secrets, ServiceAccounts, Roles, etc. exist (dry-run does not verify these).          | `kubectl get …` for each reference                                           |
| **5** | Namespace readiness: list namespaces declared in YAML and ensure they exist (or plan to create them) before apply.                     | `grep -rh "namespace:"                                                       |
| **6** | Ordering: same bundle may need multiple applies or ordered dirs (namespaces, CRDs, then dependents).                                   | Repeat Step 2 or real `kubectl apply` twice as in the doc                    |


---

## Workload A — Happy path

**Resources:** Namespace `cv-pass`, ConfigMap `app-config`, Secret `app-secret`, ServiceAccount `app-sa`, Deployment `cv-app`, Service `cv-app-svc`.

**Setup note:** `cv-pass` was created on the cluster once so **Step 2** could succeed for namespaced objects (server dry-run does not persist a namespace created in the same invocation for sibling objects).


| #   | Step (detail)                                                                                                                                                                              | Result                                                                         |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| 1   | Client dry-run over all YAML; catches syntax / local schema issues.                                                                                                                        | **Pass** (exit 0)                                                              |
| 2   | Server dry-run against the live API for the same tree.                                                                                                                                     | **Pass** (exit 0) once `cv-pass` already existed on the cluster                |
| 3   | `can-i create` for `namespaces`, `configmaps`, `secrets`, `serviceaccounts`, `deployments`, `services` (ns `cv-pass` where applicable).                                                    | **Pass** — all returned `yes`                                                  |
| 4   | Deployment references CM/Secret/SA in the same bundle.                                                                                                                                     | **Pass** — refs defined alongside                                              |
| 5   | `grep` namespaces in tree; `cv-pass` present on cluster.                                                                                                                                   | **Pass**                                                                       |
| 6   | Single recursive dry-run after ns exists behaves; without pre-created ns, namespaced objects in Step 2 can fail with `namespaces "cv-pass" not found` in the same invocation as Namespace. | **Note** — matches doc ordering guidance (namespace before namespaced objects) |


**Representative Step 1 output:**

```text
namespace/cv-pass configured (dry run)
deployment.apps/cv-app created (dry run)
service/cv-app-svc created (dry run)
```

---

## Workload B — YAML / schema failure

**Resources:** Single invalid YAML (`bad.yaml`).


| #   | Step (detail)                                                          | Result                                                                    |
| --- | ---------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| 1   | Client dry-run should hit parse / conversion errors before the server. | **Fail** (exit 1) — YAML parse error (`did not find expected ',' or ']'`) |
| 2   | Server path still requires valid YAML first.                           | **Fail** (exit 1) — same error                                            |
| 3   | No stable Kind/namespace to check.                                     | **Skipped**                                                               |
| 4   | No parseable refs to query.                                            | **Skipped**                                                               |
| 5   | No reliable namespace grep from valid YAML.                            | **Skipped**                                                               |
| 6   | N/A.                                                                   | **Skipped**                                                               |


**Key output:**

```text
error: error parsing ... bad.yaml: ... yaml: line 7: did not find expected ',' or ']'
```

---

## Workload C — API / resource mapping failure

**Resources:** `extensions/v1beta1` `Ingress` in `cv-pass`.


| #   | Step (detail)                                                | Result                                                                              |
| --- | ------------------------------------------------------------ | ----------------------------------------------------------------------------------- |
| 1   | Client resolution of GVK against discovery / built-in types. | **Fail** (exit 1) — `no matches for kind "Ingress" in version "extensions/v1beta1"` |
| 2   | Server validation for the same manifest.                     | **Fail** (exit 1) — same error                                                      |
| 3   | `auth can-i` for resources in bundle.                        | **Skipped** — not run after Step 1–2 failure                                        |
| 4   | Dependency `get` checks.                                     | **Skipped**                                                                         |
| 5   | Namespace grep / existence.                                  | **Skipped**                                                                         |
| 6   | Ordering / second pass.                                      | **Skipped**                                                                         |


**Doc gap:** The validation doc sometimes implies only Step 2 sees removed APIs; here **Step 1 failed too** with the same pattern. The main doc now notes overlap between Step 1 and Step 2 for `no matches for kind`.

---

## Workload D — Missing CRD

**Resources:** `NonExistentWidget` (`crane.validate.example/v1alpha1`).


| #   | Step (detail)                                              | Result                                       |
| --- | ---------------------------------------------------------- | -------------------------------------------- |
| 1   | Client cannot map Kind to a known API without CRD/install. | **Fail** (exit 1) — same class as Workload C |
| 2   | Server cannot accept unknown type.                         | **Fail** (exit 1)                            |
| 3   | `auth can-i` for resources in bundle.                      | **Skipped** — not run after Step 1–2 failure |
| 4   | Dependency `get` checks.                                   | **Skipped**                                  |
| 5   | Namespace grep / existence.                                | **Skipped**                                  |
| 6   | Ordering / second pass.                                    | **Skipped**                                  |


**Key output:**

```text
no matches for kind "NonExistentWidget" in version "crane.validate.example/v1alpha1"
ensure CRDs are installed first
```

---

## Workload E — Permission denial (Step 3)

**Resources:** `ClusterRole` `cv-pass-test-role`, `ConfigMap` `e-test-cm` in `cv-pass`.


| #   | Step (detail)                                                                                                                                                                                                   | Result                                                  |
| --- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------- |
| 1   | Client dry-run for ClusterRole + ConfigMap.                                                                                                                                                                     | **Pass**                                                |
| 2   | Server dry-run (as cluster admin).                                                                                                                                                                              | **Pass**                                                |
| 3   | Manual `can-i`: cluster-scoped `clusterroles`; namespaced `configmaps` in `cv-pass`. As admin — both `yes`. With `--as=system:serviceaccount:kube-system:default` — `**no`** for both (denied on test cluster). | **Scenario demonstrated** — restricted identity blocked |
| 4   | Refs in manifests exist or N/A for these objects.                                                                                                                                                               | **—**                                                   |
| 5   | `cv-pass` in ConfigMap manifest; ns present.                                                                                                                                                                    | **Pass**                                                |
| 6   | No ordering conflict observed in this small set.                                                                                                                                                                | **—**                                                   |


---

## Workload F — Missing cross-references (Step 4)

**Resources:** Deployment with bogus ConfigMap/Secret/SA; RoleBinding to missing Role; Namespace `cv-pass`.


| #   | Step (detail)                                                            | Result                                                                                       |
| --- | ------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------- |
| 1   | Client dry-run for Deployment, Namespace, RoleBinding.                   | **Pass**                                                                                     |
| 2   | Server dry-run does not resolve pod template refs or RoleBinding’s Role. | **Pass** (exit 0)                                                                            |
| 3   | `can-i create` for `deployments`, `namespaces`, `rolebindings` as admin. | **Pass** — all `yes`                                                                         |
| 4   | Manual `kubectl get` for referenced objects.                             | **Fails as designed** — e.g. `kubectl get configmap no-such-configmap -n cv-pass` → NotFound |
| 5   | `grep` shows `cv-pass`; namespace exists.                                | **Pass**                                                                                     |
| 6   | Apply order not stressed beyond normal recursive apply.                  | **—**                                                                                        |


---

## Workload G — Namespace readiness (Step 5)

**Resources:** ConfigMap in `cv-fail` only (namespace not in bundle, not pre-created).


| #   | Step (detail)                                                                                           | Result                                                            |
| --- | ------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| 1   | Client dry-run does not require the namespace object to exist on the cluster.                           | **Pass** (exit 0)                                                 |
| 2   | Server must target an existing namespace for namespaced create.                                         | **Fail** (exit 1) — `namespaces "cv-fail" not found`              |
| 3   | `auth can-i create configmaps -n cv-fail`.                                                              | **Pass** (`yes`) — permission does not imply namespace exists     |
| 4   | No cross-ref checks targeted for this workload.                                                         | **—**                                                             |
| 5   | `grep` for `namespace:` in the workload dir lists `cv-fail`; operator must create it before real apply. | **Pass** — readiness issue surfaced via grep + Step 2, not Step 1 |
| 6   | Single recursive apply order not the focus here.                                                        | **—**                                                             |


---

## Workload H — Ordering (CR before CRD in file order)

**Resources:** `00-namespace.yaml`, `a-cr-first.yaml` (CR instance), `z-crd.yaml` (CRD).


| #   | Step (detail)                                                                                                                                                                                 | Result                                                    |
| --- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------- |
| 1   | Recursive client apply order hits CR before CRD is registered in this run.                                                                                                                    | **Fail** (exit 1) — `no matches for kind "OrderTest"`     |
| 2   | Same ordering issue on server dry-run.                                                                                                                                                        | **Fail** (exit 1)                                         |
| 3   | `can-i create namespaces`, `customresourcedefinitions`; `ordertests` in `cv-pass` returned `yes` but with **Warning: the server doesn't have a resource type 'ordertests'** until CRD exists. | **Pass with warning** — re-check after CRD is established |
| 4   | No separate `kubectl get` dependency matrix for this workload.                                                                                                                                | **—**                                                     |
| 5   | Namespace `cv-pass` in tree; exists from other workloads / cluster.                                                                                                                           | **Pass**                                                  |
| 6   | Second **server** dry-run pass on the same dir: CR still fails because dry-run does not leave CRD established like a real apply.                                                              | **Still fail** on CR in dry-run                           |


**Doc nuance:** “Apply twice” fixes **real** cluster state; **two-pass server dry-run** may not validate CR+CRD ordering unless the CRD is already on the cluster. Prefer CRD first + wait, then CR, or split directories.

---

## Consolidated summary


| Workload | 1 Client dry-run | 2 Server dry-run | 3 `auth can-i`                                               | 4 Deps             | 5 Namespaces                     | 6 Ordering / notes                                     |
| -------- | ---------------- | ---------------- | ------------------------------------------------------------ | ------------------ | -------------------------------- | ------------------------------------------------------ |
| A        | Pass             | Pass*            | Pass                                                         | Pass               | Pass                             | *Pre-create ns for Step 2 with sibling namespaced objs |
| B        | Fail             | Fail             | —                                                            | —                  | —                                | —                                                      |
| C        | Fail             | Fail             | —                                                            | —                  | —                                | Overlap with Step 2 in doc                             |
| D        | Fail             | Fail             | —                                                            | —                  | —                                | —                                                      |
| E        | Pass             | Pass             | Denied with `--as` SA                                        | —                  | —                                | —                                                      |
| F        | Pass             | Pass             | Pass (admin)                                                 | Missing refs found | —                                | —                                                      |
| G        | Pass             | Fail             | `yes` for `configmaps` in `cv-fail` (permission ≠ existence) | —                  | `cv-fail` listed; not on cluster | Step 1 alone insufficient                              |
| H        | Fail             | Fail             | CRD + ordertests (warning)                                   | —                  | —                                | Two-pass **dry-run** ≠ two-pass **apply** for CRD+CR   |


