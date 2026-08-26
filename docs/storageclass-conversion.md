# StorageClass Conversion

Migrate PVC data from one StorageClass to another using crane's pipeline.

## Prerequisites

- Source and destination StorageClasses must be compatible (both block or both file, same access mode)
- On OpenShift, use `--endpoint route`. On Kubernetes, use `--endpoint nginx-ingress` with `--subdomain`
- The workload should be scaled down before transfer for data consistency (especially for databases)

## Workflow

This workflow transfers a PVC to a new name on a different StorageClass, then updates the workload's PVC reference using `pvc-rename-map`.

### 1. Scale down the workload

```bash
kubectl scale deployment/mongodb --replicas=0 -n myapp
kubectl wait --for=delete pod -l app=mongodb -n myapp --timeout=120s
```



### 2. Transfer PVC data to a new name on the target StorageClass

```bash
crane transfer-pvc \
  --source-context mycluster \
  --destination-context mycluster \
  --pvc-name mongodb-data:mongodb-data-new \
  --pvc-namespace myapp \
  --dest-storage-class gp3-csi \
  --endpoint route \
  --verify
```

This creates `mongodb-data-new` on StorageClass `gp3-csi` and copies all data from `mongodb-data` via rsync.

Use `--verify` to validate checksums after transfer.

### 3. Export and transform with pvc-rename-map

```bash
crane export --context mycluster --namespace myapp --export-dir ./export

crane transform \
  --export-dir ./export \
  --transform-dir ./transform \
  --optional-flags '{"pvc-rename-map":"mongodb-data:mongodb-data-new"}'

crane apply \
  --transform-dir ./transform \
  --output-dir ./output
```

The `pvc-rename-map` flag tells the KubernetesPlugin to rewrite `claimName` references in all workloads (Deployments, StatefulSets, Jobs, CronJobs, DaemonSets) from `mongodb-data` to `mongodb-data-new`.

### 4. Verify the transform

Check that `output/output.yaml` references the new PVC name:

```bash
grep 'mongodb-data-new' output/output.yaml
```



### 5. Apply the updated manifests

```bash
kubectl apply -f output/output.yaml -n myapp
```



### 6. Scale up and validate

```bash
kubectl scale deployment/mongodb --replicas=1 -n myapp
kubectl wait --for=condition=Available deployment/mongodb -n myapp --timeout=300s
```

Verify the application is healthy and data is intact.

### 7. Clean up the old PVC

Once confirmed, delete the original PVC:

```bash
kubectl delete pvc mongodb-data -n myapp
```



## Verify the result

```bash
# Confirm new PVC is Bound on the target StorageClass
kubectl get pvc mongodb-data-new -n myapp -o jsonpath='{.spec.storageClassName}'
# Expected: gp3-csi

# Confirm the workload mounts the new PVC
kubectl get deployment mongodb -n myapp -o jsonpath='{.spec.template.spec.volumes[*].persistentVolumeClaim.claimName}'
# Expected: mongodb-data-new
```



## Same-name workflow

If the workload must keep the original PVC name (e.g., for GitOps or StatefulSet compatibility), use this approach. It transfers data twice — once to a temporary PVC, then back to the original name on the new StorageClass.

Do not use `pvc-rename-map` for this path. Manifests keep the original `claimName` unchanged.

### 1. Scale down the workload

```bash
kubectl scale deployment/mongodb --replicas=0 -n myapp
kubectl wait --for=delete pod -l app=mongodb -n myapp --timeout=120s
```



### 2. Transfer to a temporary PVC on the new StorageClass

```bash
crane transfer-pvc \
  --source-context mycluster \
  --destination-context mycluster \
  --pvc-name mongodb-data:mongodb-data-temp \
  --pvc-namespace myapp \
  --dest-storage-class gp3-csi \
  --endpoint route \
  --verify
```



### 3. Delete the original PVC

```bash
kubectl delete pvc mongodb-data -n myapp --wait=true
```



### 4. Transfer back to the original name

```bash
crane transfer-pvc \
  --source-context mycluster \
  --destination-context mycluster \
  --pvc-name mongodb-data-temp:mongodb-data \
  --pvc-namespace myapp \
  --dest-storage-class gp3-csi \
  --endpoint route \
  --verify
```



### 5. Delete the temporary PVC

```bash
kubectl delete pvc mongodb-data-temp -n myapp --wait=true
```



### 6. Scale up and validate

```bash
kubectl scale deployment/mongodb --replicas=1 -n myapp
```

Verify the application is healthy, data is intact, and the PVC uses the new StorageClass:

```bash
kubectl get pvc mongodb-data -n myapp -o jsonpath='{.spec.storageClassName}'
# Expected: gp3-csi
```

No manifest changes are needed — the workload still references `mongodb-data` as before.

> **Note:** This copies data twice. For PVCs over 100GB, the total transfer time is roughly double the standard workflow. Consider the standard workflow (with rename + `pvc-rename-map`) if transfer time is a concern.



### StatefulSet considerations

For StatefulSets, run the same steps for each ordinal PVC (`data-<sts>-0`, `data-<sts>-1`, etc.). The `volumeClaimTemplates.storageClassName` field is immutable — to update it for future replicas, delete and recreate the StatefulSet with `--cascade=orphan` (keeps existing pods and PVCs) and apply an updated spec with the new StorageClass.

## Cross-cluster variant (example)

The same workflow works across clusters. Replace `mycluster` with separate source and target contexts, and remap the namespace if needed:

```bash
crane transfer-pvc \
  --source-context source-cluster \
  --destination-context target-cluster \
  --pvc-name mongodb-data:mongodb-data-new \
  --pvc-namespace source-ns:target-ns \
  --dest-storage-class gp3-csi \
  --endpoint route \
  --verify
```



## Indirect transfer variant (example)

For clusters without direct network connectivity, use indirect transfer via S3-compatible storage:

```bash
crane transfer-pvc \
  --source-context source-cluster \
  --destination-context target-cluster \
  --pvc-name mongodb-data:mongodb-data-new \
  --pvc-namespace source-ns:target-ns \
  --dest-storage-class gp3-csi \
  --cloud-storage "remote:my-bucket" \
  --rclone-config-file rclone.conf \
  --verify
```

No endpoint or ingress setup required. Both clusters only need outbound access to the S3 bucket.