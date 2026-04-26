# Stateful Migration Tutorial

This tutorial covers migrating an application with persistent volumes (PVCs) between Kubernetes clusters, including data transfer.

## Scenario

You have a database or other stateful application with PersistentVolumeClaims that needs to be migrated to a new cluster. This involves both migrating the Kubernetes manifests and transferring the actual volume data.

## Prerequisites

- [Crane installed](./installation.md)
- `kubectl` configured with access to both source and target clusters
- Network connectivity between clusters (for PVC data transfer)
- For `route` endpoint: OpenShift cluster as destination
- For `nginx-ingress` endpoint: nginx ingress controller on destination cluster

## Overview

Stateful migration adds a data transfer step to the standard pipeline:

```text
1. crane export      — Export resource manifests
2. crane transform   — Clean and transform manifests
3. crane transfer-pvc — Transfer PVC data between clusters
4. crane apply       — Generate final manifests
5. crane validate    — Validate against target cluster
6. kubectl apply     — Deploy to target cluster
```

## Step 1: Export Resources

```bash
export SOURCE_CTX=source-cluster
export TARGET_CTX=target-cluster
export APP_NS=my-database

crane export -n $APP_NS --context $SOURCE_CTX --export-dir migration/export
```

## Step 2: Transform Resources

```bash
crane transform --export-dir migration/export --transform-dir migration/transform
```

## Step 3: Transfer PVC Data

This is the key step that differentiates stateful migration. `crane transfer-pvc` uses rsync over an encrypted stunnel connection to transfer volume data.

### Identify PVCs to Transfer

```bash
kubectl --context $SOURCE_CTX -n $APP_NS get pvc
```

### Transfer a PVC

```bash
# Using OpenShift route endpoint
crane transfer-pvc \
  --source-context $SOURCE_CTX \
  --destination-context $TARGET_CTX \
  --pvc-name my-data-pvc \
  --pvc-namespace $APP_NS \
  --endpoint route

# Using nginx ingress endpoint
crane transfer-pvc \
  --source-context $SOURCE_CTX \
  --destination-context $TARGET_CTX \
  --pvc-name my-data-pvc \
  --pvc-namespace $APP_NS \
  --endpoint nginx-ingress \
  --subdomain my-transfer.example.com \
  --ingress-class nginx
```

### Transfer with Different Names/Namespaces

Map source PVC names and namespaces to different destination values:

```bash
crane transfer-pvc \
  --source-context $SOURCE_CTX \
  --destination-context $TARGET_CTX \
  --pvc-name source-data:dest-data \
  --pvc-namespace source-ns:dest-ns \
  --endpoint route
```

### Transfer with Custom Storage

Specify a different storage class or size on the destination:

```bash
crane transfer-pvc \
  --source-context $SOURCE_CTX \
  --destination-context $TARGET_CTX \
  --pvc-name my-data-pvc \
  --pvc-namespace $APP_NS \
  --dest-storage-class gp3 \
  --dest-storage-requests 50Gi \
  --endpoint route
```

### Verify Data Transfer

Use checksum verification to ensure data integrity:

```bash
crane transfer-pvc \
  --source-context $SOURCE_CTX \
  --destination-context $TARGET_CTX \
  --pvc-name my-data-pvc \
  --pvc-namespace $APP_NS \
  --endpoint route \
  --verify
```

## Step 4: Apply and Deploy

```bash
# Generate final manifests
crane apply --transform-dir migration/transform --output-dir migration/output

# Validate against target
crane validate --input-dir migration/output --context $TARGET_CTX

# Deploy (PVC already exists from transfer step)
kubectl --context $TARGET_CTX apply -f migration/output/output.yaml
```

## Downtime Considerations

PVC data transfer happens while the source application is running, but there are considerations:

1. **Quiesce the source application** before the final transfer to avoid data inconsistency
2. **Scale down** source Pods writing to the PVC if possible
3. **Verify** data after transfer before cutting over traffic
4. Consider running the transfer twice:
   - First pass: bulk transfer while application is running
   - Second pass: final sync after quiescing the application

## How It Works

1. Crane creates a PVC on the destination cluster matching the source PVC spec
2. An rsync daemon Pod is created in the destination namespace
3. A public endpoint (route or ingress) is created in the destination cluster
4. Self-signed TLS certificates are generated for encrypted transport
5. An rsync client Pod is created in the source namespace
6. Data is transferred from source to destination via the encrypted stunnel connection
7. Transfer Pods, Secrets, and endpoints are cleaned up after completion

## Troubleshooting

### Transfer hangs at endpoint health check

- Verify the endpoint type is supported by the destination cluster
- For `nginx-ingress`: ensure the ingress controller is running
- For `route`: ensure you're targeting an OpenShift cluster
- Check network connectivity between clusters

### Permission errors

- The source PVC must be accessible (check Pod binding)
- Ensure RBAC permissions for creating Pods, Secrets, and endpoints

### PVC name too long

Crane automatically handles names exceeding 63 characters by generating an MD5 hash.

## Next Steps

- [crane transfer-pvc Reference](./commands/transfer-pvc.md) — Full flag reference
- [State Migration Concepts](./state-migration.md) — How PVC transfer works under the hood
- [Troubleshooting](./troubleshooting.md) — Common issues and solutions
