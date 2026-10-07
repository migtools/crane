# Validating crane output manifests before applying to a target cluster

After running the crane pipeline (`export` -> `transform` -> `apply`), the output directory contains final YAML manifests ready for deployment. Before running `kubectl apply` or `oc apply` on the target cluster, it is strongly recommended to validate these manifests to catch issues early and avoid partial or broken deployments.

This document covers validation techniques using standard Kubernetes tooling. A dedicated `crane validate` subcommand is planned for a future release.

---

## Prerequisites

- `kubectl` (or `oc`) configured and pointing at the **target** cluster
- Completed crane pipeline with output manifests in the output directory (default: `./output`)
- Sufficient cluster access to run dry-run and auth checks

---

## Validation steps

### Step 1: YAML syntax validation (client-side dry-run)

Client-side dry-run parses manifests locally and validates them against the built-in OpenAPI schema bundled with your kubectl version. This catches:

- YAML syntax errors (bad indentation, missing colons, duplicate keys)
- Unknown or misspelled fields
- Type mismatches (string where int is expected, etc.)

```bash
# Validate all manifests in the output directory
kubectl apply --dry-run=client -f ./output/resources/ --recursive 2>&1
```

**What it does NOT catch:** CRD-based resources (kubectl's built-in schema doesn't know about custom resources), server-side admission webhooks, or quota limits.

**Overlap with Step 2:** Depending on kubectl version and how resources are resolved, client dry-run may already fail with `no matches for kind ... in version ...` for API versions removed on the cluster or for custom resources whose CRD is missing. Treat that the same as an API/compatibility problem and fix manifests or install CRDs before relying on Step 2 alone.

---

### Step 2: API compatibility and admission validation (server-side dry-run)

Server-side dry-run sends the manifests to the target cluster's API server, which validates them against the actual API versions, CRDs, and admission controllers installed on that cluster -- without persisting any changes.

This catches everything from Step 1, plus:

- Removed or unavailable API versions (e.g., `extensions/v1beta1` Ingress on a cluster that only supports `networking.k8s.io/v1`)
- Missing CRDs (if a manifest references a custom resource whose CRD is not installed)
- Admission webhook rejections (e.g., OPA/Gatekeeper policies, Pod Security Admission)
- Resource quota violations
- Immutable field conflicts (if the resource already exists on the target)

```bash
# Validate against the live target cluster API
kubectl apply --dry-run=server -f ./output/resources/ --recursive 2>&1
```

**Interpreting errors:**


| Error pattern                                                           | Meaning                                                                |
| ----------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `error: unable to recognize ... no matches for kind "X" in version "Y"` | API version not available on target cluster; resource needs conversion |
| `admission webhook "X" denied the request`                              | An admission controller rejected the manifest; review the policy       |
| `exceeded quota`                                                        | Namespace or cluster quota would be exceeded                           |
| `field is immutable`                                                    | Resource exists on target and this field cannot be changed via apply   |


---

### Step 3: Permission pre-check

Before applying, verify that your current kube identity has the required permissions to create/update every resource in the output. This avoids partial applies where some resources succeed and others fail due to RBAC.

`kubectl auth can-i` expects the **plural API resource name** (for example `deployments`, not `Deployment`). If you are unsure of the plural name or API group for a Kind, list resources on the cluster:

```bash
kubectl api-resources
# OpenShift: same, or use oc api-resources
```

**Rules of thumb:**

- Use verb **`create`** for net-new resources; if manifests may update existing objects, also check **`patch`** or **`update`** as appropriate.
- **Namespaced** objects: pass `-n <namespace>` using the namespace from `metadata.namespace` in each manifest (omit only when the Kind is cluster-scoped).
- **Cluster-scoped** objects: do not pass `-n`.

Common Kind → plural mappings (verify with `kubectl api-resources` if your Kind is not listed):

| Kind                       | Plural resource name (typical) | Cluster-scoped |
| -------------------------- | ------------------------------- | -------------- |
| Namespace                  | namespaces                      | yes            |
| ConfigMap                  | configmaps                      | no             |
| Secret                     | secrets                         | no             |
| ServiceAccount             | serviceaccounts                 | no             |
| Service                    | services                        | no             |
| Deployment                 | deployments                     | no             |
| StatefulSet                | statefulsets                    | no             |
| DaemonSet                  | daemonsets                      | no             |
| Ingress                    | ingresses                       | no             |
| Role / RoleBinding         | roles / rolebindings            | no             |
| ClusterRole / ClusterRoleBinding | clusterroles / clusterrolebindings | yes      |
| PersistentVolumeClaim      | persistentvolumeclaims          | no             |
| PersistentVolume           | persistentvolumes               | yes            |
| CustomResourceDefinition   | customresourcedefinitions       | yes            |
| StorageClass               | storageclasses                  | yes            |
| PriorityClass              | priorityclasses                 | yes            |
| Route (OpenShift)          | routes                          | no             |

For **custom resources** (Kinds defined by a CRD), the plural is defined on the CRD (`spec.names.plural`). Example:

```bash
kubectl auth can-i create widgets -n my-namespace
```

**Manual procedure:** For each distinct `(Kind, namespace or cluster)` in your manifests, run:

```bash
kubectl auth can-i create <plural> -n <namespace>   # namespaced
kubectl auth can-i create <plural>                  # cluster-scoped
```

Expect `yes` for every line before a real apply. A `no` means RBAC will block that object.

**Example** for manifests that define Namespace `my-app`, plus a Deployment and Service in `my-app`:

```bash
kubectl auth can-i create namespaces
kubectl auth can-i create deployments -n my-app
kubectl auth can-i create services -n my-app
```

**Optional:** Impersonate another identity to validate what a migration user can do (requires impersonation rights):

```bash
kubectl auth can-i create deployments -n my-app --as=system:serviceaccount:my-app:deployer
```

---

### Step 4: Dependency and reference checks

Server-side dry-run does **not** fully validate cross-resource references. A Deployment referencing a non-existent ConfigMap or Secret will pass dry-run but fail at pod scheduling time.

Common cross-resource references to check:


| Resource                             | References to verify                                              |
| ------------------------------------ | ----------------------------------------------------------------- |
| Deployment / StatefulSet / DaemonSet | ConfigMaps, Secrets, ServiceAccounts, PVCs referenced in pod spec |
| Ingress                              | Services referenced in backend rules                              |
| RoleBinding / ClusterRoleBinding     | Roles/ClusterRoles and subjects (ServiceAccounts, Users, Groups)  |
| Service                              | Target pods exist (matched by selector)                           |
| PersistentVolumeClaim                | StorageClass exists on target                                     |


**Manual verification approach:**

```bash
# Check that referenced ConfigMaps exist
kubectl get configmap <name> -n <namespace>

# Check that referenced Secrets exist
kubectl get secret <name> -n <namespace>

# Check that referenced ServiceAccounts exist
kubectl get serviceaccount <name> -n <namespace>

# Check that the StorageClass referenced by PVCs exists
kubectl get storageclass <name>
```

When migrating with crane, most of these resources should already be in the output directory (crane exports them together). The main risk is references to cluster-scoped resources or resources that were excluded/whited-out during the transform step.

---

### Step 5: Namespace readiness

Ensure target namespaces exist before applying namespace-scoped resources:

```bash
# List namespaces referenced in the output manifests
grep -rh "namespace:" ./output/resources/ | sort -u

# Create any missing namespaces (or apply namespace manifests first)
kubectl create namespace <namespace> --dry-run=server -o yaml | kubectl apply -f -
```

If crane exported namespace resources, apply those first:

```bash
kubectl apply -f ./output/resources/namespaces/ 2>/dev/null || true
```

---

### Step 6: Resource ordering

**Note:** `crane apply` does not deploy resources to a cluster -- it only applies JSONPatch transformations to exported YAML and writes the resulting manifests to the output directory. It processes files in alphabetical directory order (via Go's `ioutil.ReadDir`) and does not sort by resource kind. Resource ordering is entirely a concern for the subsequent `kubectl apply` (or `oc apply`) step that actually deploys to the target cluster.

When running `kubectl apply -f ./output/resources/ --recursive`, kubectl sends all resources to the API server but does not guarantee a specific ordering. In practice, most resources are accepted regardless of order because the API server validates the manifest itself, not its runtime dependencies. However, certain resources **must** exist before others can reference them:

- **Namespaces** must exist before any namespace-scoped resource can be created in them
- **CRDs** must be registered before any custom resource instances can be created
- **ServiceAccounts** referenced in RBAC bindings should exist (though bindings can reference non-existent subjects)

For a reliable migration, apply resources in this order:

1. **Namespaces**
2. **CustomResourceDefinitions** (wait for them to become established: `kubectl wait --for=condition=established crd/<name>`)
3. **StorageClasses, PriorityClasses** (cluster-scoped infrastructure)
4. **ServiceAccounts, Secrets, ConfigMaps** (referenced by pods)
5. **RBAC** (Roles, RoleBindings, ClusterRoles, ClusterRoleBindings)
6. **PersistentVolumeClaims**
7. **Services** (so Deployments can reference them via env vars)
8. **Deployments, StatefulSets, DaemonSets, Jobs, CronJobs**
9. **Ingresses, Routes, NetworkPolicies**

If your output directory has a flat structure and you are applying everything at once, you can work around ordering issues by running `kubectl apply` twice -- the second pass picks up resources that failed on the first pass due to missing dependencies:

```bash
kubectl apply -f ./output/resources/ --recursive 2>&1
# Re-apply to resolve ordering-dependent failures
kubectl apply -f ./output/resources/ --recursive 2>&1
```

---

## Recommended validation workflow

Combine all steps into a single pre-apply validation pass:

```bash
OUTPUT_DIR="./output/resources"

echo "=== Step 1: Client-side syntax validation ==="
kubectl apply --dry-run=client -f "$OUTPUT_DIR" --recursive 2>&1

echo ""
echo "=== Step 2: Server-side API validation ==="
kubectl apply --dry-run=server -f "$OUTPUT_DIR" --recursive 2>&1

echo ""
echo "=== Step 3: Permission check (run kubectl auth can-i for each Kind in your manifests; see Step 3) ==="
# Example — replace with the plural names and namespaces from your YAML:
# kubectl auth can-i create namespaces
# kubectl auth can-i create deployments -n my-namespace
```

If steps 1–2 pass with no errors and every Step 3 `can-i` returns `yes`, the manifests are ready for `kubectl apply -f ./output/resources/ --recursive` (subject to dependency and ordering checks in steps 4–6).

---

## Limitations of pre-apply validation

- **Runtime dependencies** cannot be fully validated before apply. For example, a pod readiness probe that depends on an external service will only fail after the pod is scheduled.
- **Server-side dry-run** does not execute init containers, jobs, or any controller logic -- it only validates the API request.
- **Cross-resource references** (ConfigMap/Secret mounts, Service selectors) are not checked by dry-run. The dependency check in Step 4 is manual and best-effort.
- **Ordering sensitivity** means that some resources may fail validation if their dependencies are not yet present on the cluster (e.g., a RoleBinding referencing a Role that is also being migrated).
