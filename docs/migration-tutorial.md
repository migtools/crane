# Crane Migration Tutorial

Migrate a Kubernetes application from a source cluster to a target cluster using crane's pipeline. This tutorial covers both stateless and stateful workloads.

Crane follows a non-destructive, auditable pipeline:

```
export → transform → apply → validate → (transfer-pvc) → kubectl apply
```

For stateless applications (no PersistentVolumes), skip the `transfer-pvc` step. Everything else is the same.

## Prerequisites

- `crane` CLI installed ([installation guide](./installation.md))
- `kubectl` on your PATH
- Kubeconfig with valid contexts for both source and target clusters
- Namespace-level access on both clusters (cluster-admin not required)
- Target namespace must already exist on the target cluster (ask your cluster admin to create it if you do not have namespace-creation privileges)
- For stateful migration: workload must be scaled down before PVC transfer

Before running any crane commands, make sure your local `kubeconfig` already includes valid contexts for both clusters. Crane runs locally and uses the `--context` flag to talk directly to each cluster using your existing Kubernetes RBAC permissions.

## Sample Application

This tutorial uses a MongoDB deployment with a PersistentVolumeClaim. Deploy it on your source cluster to follow along. For stateless migration, omit the PVC and volume mount — the rest of the tutorial still applies.

Save as `sample-app.yaml`:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: mongodb-data
  namespace: demo-app
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mongodb
  namespace: demo-app
spec:
  replicas: 1
  selector:
    matchLabels:
      app: mongodb
  template:
    metadata:
      labels:
        app: mongodb
    spec:
      containers:
        - name: mongodb
          image: mongo:6
          ports:
            - containerPort: 27017
          volumeMounts:
            - name: data
              mountPath: /data/db
      volumes:
        - name: data
          persistentVolumeClaim:
            claimName: mongodb-data
---
apiVersion: v1
kind: Service
metadata:
  name: mongodb
  namespace: demo-app
spec:
  selector:
    app: mongodb
  ports:
    - port: 27017
      targetPort: 27017
```

Deploy and seed with data:

```bash
kubectl --context src-cluster create namespace demo-app
kubectl --context src-cluster apply -f sample-app.yaml
kubectl --context src-cluster wait --for=condition=Available deployment/mongodb -n demo-app --timeout=120s

# Insert sample data
kubectl --context src-cluster exec deployment/mongodb -n demo-app -- \
  mongosh --eval 'db.items.insertMany([
    {name: "widget", qty: 25},
    {name: "gadget", qty: 50},
    {name: "sprocket", qty: 100}
  ])'
```

## Setup

Set environment variables used throughout the tutorial:

```bash
export SOURCE_CONTEXT=src-cluster
export TARGET_CONTEXT=tgt-cluster
export NAMESPACE=demo-app
```

Create a working directory for crane artifacts:

```bash
mkdir -p crane-migration && cd crane-migration
```

## Step 1: Export

`crane export` discovers resources in the source namespace and writes them as manifest files to disk. This is a read-only operation on the source cluster.

```bash
crane export \
  --context "${SOURCE_CONTEXT}" \
  -n "${NAMESPACE}" \
  -e export
```

Example output (abbreviated):

```text
INFO[0000] adding resource: secrets to the list of GVRs to be extracted
INFO[0000] adding resource: services to the list of GVRs to be extracted
INFO[0000] adding resource: deployments to the list of GVRs to be extracted
INFO[0000] adding resource: persistentvolumeclaims to the list of GVRs to be extracted
INFO[0000] No matching cluster-scoped resources found; _cluster/ directory will be empty
INFO[0000] Writing objects of resource: secrets to the output directory
INFO[0000] Writing objects of resource: services to the output directory
INFO[0000] Writing objects of resource: deployments to the output directory
INFO[0000] Writing objects of resource: persistentvolumeclaims to the output directory
```

What you can expect to see:

- Log lines showing resources discovered for extraction (for example, `secrets`, `services`, `configmaps`, `deployments`, `persistentvolumeclaims`)
- A write phase with lines like `Writing objects of resource: <name> to the output directory`
- If no cluster-scoped objects match, a message that `_cluster/` will be empty
- Exported manifests written under `export/resources/${NAMESPACE}/`
- If extraction fails for specific objects, failure artifacts are written under `export/failures/${NAMESPACE}/`

See [export command reference](./commands/export.md) for all flags.

## Step 2: Transform

`crane transform` takes exported manifests, cleans and updates them in stages, and saves the results in `transform/`.

A stage is one step in the transform pipeline. Think of it like an assembly line:
- Stage 1 takes your exported manifests and makes the first set of changes.
- Stage 2 takes Stage 1's output and applies the next changes.

Example: `10_KubernetesPlugin` runs first, then `25_CustomStage` runs on top of that result.

```bash
crane transform -e export -t transform
```

Example output (abbreviated):

```text
INFO[0000] No existing stages found, creating default stages for 1 plugin(s)
INFO[0000] Creating default stage for plugin: KubernetesPlugin -> 10_KubernetesPlugin
INFO[0000] Created 1 default stage(s): [10_KubernetesPlugin]
INFO[0000] Populating and executing all default stages
INFO[0000] Executing stage 1/1: 10_KubernetesPlugin
INFO[0000] Stage 10_KubernetesPlugin: loaded 10 input resource(s)
INFO[0000] Stage 10_KubernetesPlugin: produced 4 output resource(s)
INFO[0000] Successfully completed 1 stage(s)
```

What you can expect to see:

- Default stage creation logs when no stages exist yet
- Stage execution progress (for example, `Executing stage 1/1: 10_KubernetesPlugin`)
- Input and output resource counts per stage
- A success line indicating all stages completed
- Stage artifacts in `transform/10_KubernetesPlugin/`: `input/`, `patches/`, `output/`, `kustomization.yaml`

Stage naming uses numeric prefixes to control order. `10_KubernetesPlugin` runs before `25_CustomStage`. Crane executes stages from lowest number to highest.

Expected transform directory structure:

```text
transform/
└── 10_KubernetesPlugin
    ├── input/
    │   ├── ...ConfigMap...
    │   ├── ...Deployment...
    │   ├── ...PersistentVolumeClaim...
    │   ├── ...Secret...
    │   └── ...Service...
    ├── kustomization.yaml
    ├── output/
    │   └── <namespace>/
    │       ├── ...ConfigMap...
    │       ├── ...Deployment...
    │       ├── ...PersistentVolumeClaim...
    │       ├── ...Secret...
    │       └── ...Service...
    └── patches/
        ├── ...Deployment.patch.yaml
        ├── ...PersistentVolumeClaim.patch.yaml
        ├── ...Secret.patch.yaml
        └── ...Service.patch.yaml
```

### Optional: OpenShiftPlugin

If migrating between OpenShift clusters, install the OpenShiftPlugin for additional transforms (pull secret replacement, registry rewriting, default RBAC stripping):

```bash
crane plugin-manager install OpenShiftPlugin
crane transform -e export -t transform
```

### Optional: Custom flags

Pass optional flags to plugins using `--optional-flags` with a JSON object:

```bash
crane transform -e export -t transform \
  --optional-flags '{"registry-replacement":"docker-registry.default.svc:5000=image-registry.openshift-image-registry.svc:5000"}'
```

### Optional: Custom stages

Add a custom pass-through stage for manual edits:

```bash
crane transform -e export -t transform 25_CustomStage
```

This creates `transform/25_CustomStage/` with `input/`, `output/`, and `kustomization.yaml`. Edit resources in this stage, then preview the rendered manifests before applying:

```bash
kubectl kustomize transform/25_CustomStage
```

In most pipelines, your custom stage is the last stage under `transform/`, so rendering that directory should show the manifests that will feed into the final apply output.

For a deeper explanation of stage ordering, stage structure, and multi-stage behavior, see [Multi-Stage Kustomize Transform Pipeline](./multistage-pipeline.md).

If a custom stage already contains edits and you need to regenerate stage artifacts (`input/`, `output/`, `kustomization.yaml`), rerun with `--overwrite`:

```bash
crane transform -e export -t transform --overwrite
```

### Optional: Instructions file

For repeatable, scripted migrations, drive stage behavior with an instructions file:

```bash
crane transform --instructions-file ./instructions.yaml
```

Example `instructions.yaml`:

```yaml
stages:
  - KubernetesPlugin
  - CustomStage
```

Stage directory names are auto-generated with numeric prefixes:

```text
transform/
├── 10_KubernetesPlugin/
└── 20_CustomStage/
```

Crane creates and executes stages in the order listed in the instructions file. Prefix numbers control execution order from lowest to highest.

See [transform command reference](./commands/transform.md) for all options.

## Step 3: Apply (Render Final Manifests)

`crane apply` renders the final manifests from transform stages into deployable output files.

```bash
crane apply -t transform -o output
```

Example output (abbreviated):

```text
INFO[0000] Applying all stages...
INFO[0000] Applying final stage: 10_KubernetesPlugin
INFO[0000] Successfully applied final stage to .../output/output.yaml
```

This produces:
- `output/output.yaml` — all resources combined in one file (useful for a single `kubectl apply -f`)
- `output/resources/` — individual files per resource (useful for review or selective apply)

Review the output before proceeding:

```bash
cat output/output.yaml
```

Verify that server-managed fields have been stripped and the manifests look clean.

For non-admin scenarios, skip cluster-scoped resources:

```bash
crane apply -t transform -o output --skip-cluster-scoped
```

See [apply command reference](./commands/apply.md) for details.

## Step 4: Validate (Recommended)

`crane validate` checks whether the rendered manifests are compatible with the target cluster's API server. This step is optional but strongly recommended before applying manifests.

```bash
crane validate \
  --context "${TARGET_CONTEXT}" \
  -i output \
  --validate-dir validate
```

Example output (abbreviated):

```text
INFO[0000] Scanned 4 distinct GVK+namespace tuples
INFO[0000] Validating in live mode against context "tgt-cluster"
Mode: live (context: tgt-cluster)
...
Summary: 4 scanned, 4 compatible, 0 incompatible
Result: PASSED — all resources compatible with target cluster
```

If validation passes, you will see `Result: PASSED`. If any resources are incompatible (e.g., an API version not available on the target), fix the transforms and re-run steps 2-4.

The validation report is written to `validate/report.json`:

```json
{
  "mode": "live",
  "clusterContext": "tgt-cluster",
  "results": [
    {
      "apiVersion": "apps/v1",
      "kind": "Deployment",
      "namespace": "demo-app",
      "resourcePlural": "deployments",
      "status": "OK"
    }
  ],
  "totalScanned": 4,
  "compatible": 4,
  "incompatible": 0
}
```

If incompatibilities are found, failure artifacts are written under `validate/failures/`.

See [validate command reference](./commands/validate.md) for details.

## Step 5: Transfer PVC Data (Stateful Only)

> **Skip this step for stateless applications.** If your application has no PersistentVolumeClaims, proceed directly to Step 6.

`crane transfer-pvc` copies PVC data from the source cluster to the target cluster using rsync over an encrypted stunnel connection.

### 5a. Ensure the target namespace exists

The target namespace must exist before transferring PVC data. If it does not exist, a cluster admin should create it:

```bash
# Requires cluster-admin or namespace-creation privileges
kubectl --context "${TARGET_CONTEXT}" create namespace "${NAMESPACE}"
```

If you are a non-admin user, ask your cluster admin to create the namespace and grant you the necessary RBAC permissions on it before proceeding.

### 5b. Scale down the workload

The source PVC must be unmounted before transfer. Scale down the workload and wait for all pods to terminate:

```bash
kubectl --context "${SOURCE_CONTEXT}" scale deployment/mongodb --replicas=0 -n "${NAMESPACE}"
kubectl --context "${SOURCE_CONTEXT}" wait --for=delete pod -l app=mongodb -n "${NAMESPACE}" --timeout=120s
```

Verify no pods are running:

```bash
kubectl --context "${SOURCE_CONTEXT}" get pods -n "${NAMESPACE}" -l app=mongodb
# Expected: No resources found
```

### 5c. Transfer the PVC

Get the target cluster's node IP. This IP must be reachable from the source cluster:

```bash
NODE_IP=$(kubectl --context "${TARGET_CONTEXT}" get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
```

Transfer the PVC data:

```bash
crane transfer-pvc \
  --source-context "${SOURCE_CONTEXT}" \
  --destination-context "${TARGET_CONTEXT}" \
  --pvc-name mongodb-data \
  --pvc-namespace "${NAMESPACE}" \
  --endpoint nginx-ingress \
  --subdomain "${NODE_IP}.nip.io" \
  --ingress-class nginx \
  --verify
```

> **Minikube setup:** Both minikube clusters must share the same Docker/Podman network for the InternalIP to be reachable. Use `make setup-minikube-same-network` from this repo, or create clusters with `--network=minikube`. The nip.io wildcard DNS service resolves `<IP>.nip.io` back to the IP, providing a valid hostname for the Ingress without any DNS setup.

> **OpenShift:** Use `--endpoint route` instead of `--endpoint nginx-ingress`. No `--subdomain` or `--ingress-class` flags are needed — the cluster's default router subdomain is used automatically.

Key flags:
- `--pvc-name` — name of the PVC to transfer (use `source:destination` format to rename, e.g. `mongodb-data:mongodb-data-new`)
- `--pvc-namespace` — namespace (use `source:destination` format for cross-namespace, e.g. `source-ns:target-ns`)
- `--dest-storage-class` — provision the destination PVC on a different StorageClass
- `--endpoint` — `nginx-ingress` for Kubernetes, `route` for OpenShift
- `--ingress-class` — ingress class name (required with `nginx-ingress`)
- `--subdomain` — wildcard DNS subdomain (required with `nginx-ingress`)
- `--verify` — validates transferred files using checksums after transfer

The transfer creates temporary resources (rsync pods, services, secrets, ingresses) on both clusters. These are cleaned up automatically when the transfer completes.

### 5d. Verify the transfer

Confirm the destination PVC was created and is Bound:

```bash
kubectl --context "${TARGET_CONTEXT}" get pvc mongodb-data -n "${NAMESPACE}"
# Expected: STATUS = Bound
```

See [transfer-pvc command reference](./commands/transfer-pvc.md) for all flags.

## Step 6: Apply to Target Cluster

Ensure the target namespace exists. For stateful migrations, this was already done in Step 5a. For stateless migrations, create it now (requires cluster-admin or namespace-creation privileges):

```bash
kubectl --context "${TARGET_CONTEXT}" create namespace "${NAMESPACE}" --dry-run=client -o yaml | \
  kubectl --context "${TARGET_CONTEXT}" apply -f -
```

Apply the rendered manifests:

```bash
kubectl --context "${TARGET_CONTEXT}" apply -f output/output.yaml -n "${NAMESPACE}"
```

For stateful applications, the Deployment will start and mount the PVC that `transfer-pvc` already created in Step 5. For stateless applications, the Deployment starts immediately with no PVC dependency.

> **Warning — Namespace renaming:** If you renamed the namespace during migration, crane does not automatically update **ClusterRoleBinding subjects** referencing the old namespace name, or **NetworkPolicy `namespaceSelector`** entries matching the old namespace by label (e.g., `kubernetes.io/metadata.name: old-ns`). These will silently point to a namespace that no longer exists. Manually update them before applying.

## Step 7: Verify

Check that the application is running:

```bash
kubectl --context "${TARGET_CONTEXT}" get pods -n "${NAMESPACE}"
kubectl --context "${TARGET_CONTEXT}" wait --for=condition=Available deployment/mongodb -n "${NAMESPACE}" --timeout=300s
```

For stateful applications, verify data integrity:

```bash
kubectl --context "${TARGET_CONTEXT}" exec deployment/mongodb -n "${NAMESPACE}" -- \
  mongosh --eval 'db.items.find().pretty()'
```

You should see the same three documents that were inserted on the source cluster:

```json
{ "name": "widget", "qty": 25 }
{ "name": "gadget", "qty": 50 }
{ "name": "sprocket", "qty": 100 }
```

## Cleanup

After verifying the migration, clean up local artifacts:

```bash
rm -rf export/ transform/ output/ validate/
```

Optionally remove the application from the source cluster:

```bash
kubectl --context "${SOURCE_CONTEXT}" delete namespace "${NAMESPACE}"
```

## Advanced Topics

### StorageClass Conversion

To migrate PVC data to a different StorageClass (e.g., during a storage backend upgrade), see the [StorageClass Conversion Guide](./storageclass-conversion.md).

### Indirect Transfer (S3)

For clusters without direct network connectivity, `transfer-pvc` supports indirect transfer via S3-compatible storage:

```bash
crane transfer-pvc \
  --source-context "${SOURCE_CONTEXT}" \
  --destination-context "${TARGET_CONTEXT}" \
  --pvc-name mongodb-data \
  --pvc-namespace "${NAMESPACE}" \
  --cloud-storage "remote:my-bucket" \
  --rclone-config-file rclone.conf \
  --verify
```

Both clusters only need outbound access to the S3 bucket. No endpoint or ingress setup required. Add `--encrypt` for client-side encryption.

### PVC Rename with Workload Reference Update

To change the PVC name during migration and update all workload references:

```bash
# Transfer with rename (add your --source-context, --destination-context, --endpoint flags)
crane transfer-pvc \
  --source-context "${SOURCE_CONTEXT}" \
  --destination-context "${TARGET_CONTEXT}" \
  --pvc-name mongodb-data:mongodb-data-new \
  --pvc-namespace "${NAMESPACE}" \
  --endpoint route \
  --verify

# Transform with pvc-rename-map to rewrite claimName in Deployments, StatefulSets, etc.
crane transform -e export -t transform \
  --optional-flags '{"pvc-rename-map":"mongodb-data:mongodb-data-new"}'
```

### Non-Admin Migration

Crane does not require cluster-admin. Namespace-level permissions are sufficient. For non-admin scenarios, skip cluster-scoped resources during apply:

```bash
crane apply -t transform -o output --skip-cluster-scoped
```

See [RBAC requirements](./rbac-scc-requirements.md) for details.

### Pre-Apply Validation

For a detailed guide on validating manifests before applying them to the target cluster, see the [Pre-Apply Validation Guide](./pre-apply-validation-guide.md).

## Troubleshooting

### Export directory already exists

Use `--overwrite` with `crane export`.

### Apply or validate directory already exists

Use `--overwrite` with `crane apply` or `crane validate`.

### Existing custom stage blocks rerun

Use `crane transform --overwrite` to regenerate stage directories.

### Validation shows incompatibilities

- Check `validate/report.json` for `apiVersion`/`kind` mismatches
- Update transforms, then rerun `crane apply` and `crane validate`

### Transfer-pvc hangs or times out

- Ensure the workload is fully scaled down (`kubectl get pods` shows no running pods)
- Verify the endpoint type matches the cluster (Route for OpenShift, nginx-ingress for Kubernetes)
- Check that the ingress controller or router is running on the destination cluster
- For nginx-ingress, verify the `--subdomain` resolves to a cluster node
