# Quickstart Tutorial - Your First Crane Migration

This tutorial walks you through a complete example of using `crane` to migrate a simple WordPress application (**stateless**).

## Prerequisites

- `crane` CLI installed (see [installation guide](../../README.md#install))
- `kubectl` available in your PATH
- Access to a Kubernetes cluster (for export phase)

## Stateless Tutorial Overview

We'll migrate a WordPress + MySQL application through these steps:

1. Deploy sample application to a cluster
2. Export resources using `crane export`
3. Transform resources using `crane transform`
4. Review the transformed output
5. Apply to generate manifest
6. Deploy to target cluster (optional)


## Step 1: Deploy Sample Application

First, let's deploy a sample WordPress application to your cluster:

```bash
# Create namespace
kubectl create namespace wordpress-demo

# Deploy WordPress and MySQL
kubectl apply -n wordpress-demo -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: mysql-password
type: Opaque
stringData:
  password: changeme123
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: mysql-pvc
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
  name: mysql
spec:
  selector:
    matchLabels:
      app: mysql
  template:
    metadata:
      labels:
        app: mysql
    spec:
      containers:
      - name: mysql
        image: mysql:8.0
        env:
        - name: MYSQL_ROOT_PASSWORD
          valueFrom:
            secretKeyRef:
              name: mysql-password
              key: password
        - name: MYSQL_DATABASE
          value: wordpress
        ports:
        - containerPort: 3306
        volumeMounts:
        - name: mysql-storage
          mountPath: /var/lib/mysql
      volumes:
      - name: mysql-storage
        persistentVolumeClaim:
          claimName: mysql-pvc
---
apiVersion: v1
kind: Service
metadata:
  name: mysql
spec:
  selector:
    app: mysql
  ports:
  - port: 3306
    targetPort: 3306
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: wordpress
spec:
  selector:
    matchLabels:
      app: wordpress
  template:
    metadata:
      labels:
        app: wordpress
    spec:
      containers:
      - name: wordpress
        image: wordpress:6.4
        env:
        - name: WORDPRESS_DB_HOST
          value: mysql
        - name: WORDPRESS_DB_PASSWORD
          valueFrom:
            secretKeyRef:
              name: mysql-password
              key: password
        ports:
        - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: wordpress
spec:
  type: NodePort
  selector:
    app: wordpress
  ports:
  - port: 80
    targetPort: 80
EOF
```

Wait for pods to be ready:
```bash
kubectl wait -n wordpress-demo --for=condition=ready pod -l app=wordpress --timeout=120s
kubectl wait -n wordpress-demo --for=condition=ready pod -l app=mysql --timeout=120s
```

Verify deployment:
```bash
kubectl get all -n wordpress-demo
```

**Expected output:**
```
NAME                             READY   STATUS    RESTARTS   AGE
pod/mysql-5d8c7b9d4f-x7k2p       1/1     Running   0          30s
pod/wordpress-7b8c9d5f6-q9m3n    1/1     Running   0          30s

NAME                TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)        AGE
service/mysql       ClusterIP   10.96.123.45    <none>        3306/TCP       30s
service/wordpress   NodePort    10.96.234.56    <none>        80:30123/TCP   30s

NAME                        READY   UP-TO-DATE   AVAILABLE   AGE
deployment.apps/mysql       1/1     1            1           30s
deployment.apps/wordpress   1/1     1            1           30s

NAME                                   DESIRED   CURRENT   READY   AGE
replicaset.apps/mysql-5d8c7b9d4f       1         1         1       30s
replicaset.apps/wordpress-7b8c9d5f6    1         1         1       30s
```

## Step 2: Export Resources

Now let's export the deployed resources:

```bash
crane export -n wordpress-demo
```

**What happens:**
- Crane discovers all resources in the `wordpress-demo` namespace
- Exports each resource to a YAML file
- Saves to `export/resources/wordpress-demo/` directory

**Check the export:**
```bash
ls export/resources/wordpress-demo/
```

**Expected output:**
```
Deployment_apps_v1_wordpress-demo_mysql.yaml
Deployment_apps_v1_wordpress-demo_wordpress.yaml
Endpoints__v1_wordpress-demo_mysql.yaml
Endpoints__v1_wordpress-demo_wordpress.yaml
PersistentVolumeClaim__v1_wordpress-demo_mysql-pvc.yaml
Pod__v1_wordpress-demo_mysql-5d8c7b9d4f-x7k2p.yaml
Pod__v1_wordpress-demo_wordpress-7b8c9d5f6-q9m3n.yaml
ReplicaSet_apps_v1_wordpress-demo_mysql-5d8c7b9d4f.yaml
ReplicaSet_apps_v1_wordpress-demo_wordpress-7b8c9d5f6.yaml
Secret__v1_wordpress-demo_mysql-password.yaml
Service__v1_wordpress-demo_mysql.yaml
Service__v1_wordpress-demo_wordpress.yaml
ServiceAccount__v1_wordpress-demo_default.yaml
```

**Examine an exported resource:**
```bash
cat export/resources/wordpress-demo/Deployment_apps_v1_wordpress-demo_mysql.yaml
```

Notice the resource contains cluster-specific metadata:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mysql
  namespace: wordpress-demo
  uid: 8fb75dcd-68b2-4939-bfb9-1c8241a7b146        # ← Must be removed
  resourceVersion: "12345"                          # ← Must be removed
  creationTimestamp: "2024-04-24T10:30:00Z"        # ← Must be removed
  generation: 1
  managedFields: [...]                              # ← Must be removed
spec:
  # ... your deployment spec ...
status:                                              # ← Must be removed
  availableReplicas: 1
  # ... runtime status ...
```

## Step 3: Transform Resources

Run the transform command:

```bash
crane transform
```

**What happens:**
- No stages exist, so Crane creates default stage: `10_KubernetesPlugin`
- Loads the built-in KubernetesPlugin
- Plugin analyzes each resource
- Generates JSONPatch operations to remove server-managed fields
- Writes transformed resources and patches to `transform/10_KubernetesPlugin/`

**Expected output:**
```
INFO[0000] No existing stages found, creating default stage: 10_KubernetesPlugin
INFO[0000] Populating and executing default stage
INFO[0000] Executing stage 1/1: 10_KubernetesPlugin
INFO[0000] Stage 10_KubernetesPlugin: loaded 13 input resource(s)
INFO[0001] Stage 10_KubernetesPlugin: produced 13 output resource(s)
INFO[0001] Successfully completed 1 stage(s)
```

**Explore the generated structure:**
```bash
tree transform/10_KubernetesPlugin/
```

**Expected structure:**
```
transform/10_KubernetesPlugin/
├── kustomization.yaml
├── patches/
│   ├── wordpress-demo--apps-v1--Deployment--mysql.patch.yaml
│   ├── wordpress-demo--apps-v1--Deployment--wordpress.patch.yaml
│   ├── wordpress-demo---v1--PersistentVolumeClaim--mysql-pvc.patch.yaml
│   ├── wordpress-demo---v1--Secret--mysql-password.patch.yaml
│   ├── wordpress-demo---v1--Service--mysql.patch.yaml
│   └── wordpress-demo---v1--Service--wordpress.patch.yaml
└── resources/
    ├── Deployment_apps_v1_wordpress-demo_mysql.yaml
    ├── Deployment_apps_v1_wordpress-demo_wordpress.yaml
    ├── PersistentVolumeClaim__v1_wordpress-demo_mysql-pvc.yaml
    ├── Secret__v1_wordpress-demo_mysql-password.yaml
    ├── Service__v1_wordpress-demo_mysql.yaml
    └── Service__v1_wordpress-demo_wordpress.yaml
```

**Key points:**
- `resources/` contains the exported resources (copied from export)
- `patches/` contains JSONPatch operations generated by the plugin
- `kustomization.yaml` ties them together
- Notice: Pods, ReplicaSets, Endpoints are NOT included (handled by "whiteout" - explained later)

## Step 4: Review Transformed Output

**Examine a patch file:**
```bash
cat transform/10_KubernetesPlugin/patches/wordpress-demo--apps-v1--Deployment--mysql.patch.yaml
```

**Expected content:**
```yaml
- op: remove
  path: /metadata/uid
- op: remove
  path: /metadata/resourceVersion
- op: remove
  path: /metadata/creationTimestamp
- op: remove
  path: /metadata/generation
- op: remove
  path: /metadata/managedFields
- op: remove
  path: /status
```

These patches remove the server-managed fields we saw earlier!

**Examine the kustomization.yaml:**
```bash
cat transform/10_KubernetesPlugin/kustomization.yaml
```

**Expected content:**
```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
patches:
- path: patches/wordpress-demo--apps-v1--Deployment--mysql.patch.yaml
  target:
    group: apps
    kind: Deployment
    name: mysql
    namespace: wordpress-demo
    version: v1
- path: patches/wordpress-demo--apps-v1--Deployment--wordpress.patch.yaml
  target:
    group: apps
    kind: Deployment
    name: wordpress
    namespace: wordpress-demo
    version: v1
# ... more patches ...
resources:
- resources/Deployment_apps_v1_wordpress-demo_mysql.yaml
- resources/Deployment_apps_v1_wordpress-demo_wordpress.yaml
- resources/PersistentVolumeClaim__v1_wordpress-demo_mysql-pvc.yaml
- resources/Secret__v1_wordpress-demo_mysql-password.yaml
- resources/Service__v1_wordpress-demo_mysql.yaml
- resources/Service__v1_wordpress-demo_wordpress.yaml
```

**Preview the final output:**
```bash
kubectl kustomize transform/10_KubernetesPlugin/
```

This shows what will be deployed after patches are applied. Notice:
- No `uid` fields
- No `resourceVersion` fields
- No `status` sections
- Clean, declarative manifests ready for target cluster

**Compare before and after:**
```bash
# Before (exported resource)
grep -A 5 "metadata:" export/resources/wordpress-demo/Deployment_apps_v1_wordpress-demo_mysql.yaml

# After (transformed resource)
kubectl kustomize transform/10_KubernetesPlugin/ | grep -A 5 "kind: Deployment" | head -20
```

## Step 5: Apply and Generate Final Output

Run `crane apply` to generate the final manifests:

```bash
crane apply
```

**What happens:**
- Runs `kubectl kustomize` on the final stage
- Writes output to `output/` directory
- Creates both single-file and per-resource layouts

**Expected output:**
```
INFO[0000] Applying final stage to output directory
INFO[0000] Applied stage 10_KubernetesPlugin
INFO[0000] Wrote 6 resources to output/output.yaml
INFO[0000] Wrote 6 individual resource files to output/resources/
```

**Check the output:**
```bash
ls output/
```

**Expected structure:**
```
output/
├── output.yaml                # Single file with all resources
└── resources/                 # Individual resource files
    └── wordpress-demo/
        ├── Deployment_apps_v1_wordpress-demo_mysql.yaml
        ├── Deployment_apps_v1_wordpress-demo_wordpress.yaml
        ├── PersistentVolumeClaim__v1_wordpress-demo_mysql-pvc.yaml
        ├── Secret__v1_wordpress-demo_mysql-password.yaml
        ├── Service__v1_wordpress-demo_mysql.yaml
        └── Service__v1_wordpress-demo_wordpress.yaml
```

**Review the final output:**
```bash
cat output/output.yaml
```

This file is ready to deploy to any target cluster!

## Step 6: Deploy to Target Cluster (Optional)

If you have a target cluster, you can deploy the transformed resources:

```bash
# Create namespace on target cluster
kubectl create namespace wordpress-demo

# Apply the transformed resources
kubectl apply -f output/output.yaml

# Verify deployment
kubectl get all -n wordpress-demo
```

**Note:** For this demo, you can also apply to the same cluster (in a different namespace) to verify the transformation worked.

## Understanding What Happened

Let's trace what happened to one resource (mysql Deployment):

1. **Export phase** (`crane export`):
   - Read live Deployment from cluster
   - Saved to `export/resources/wordpress-demo/Deployment_apps_v1_wordpress-demo_mysql.yaml`
   - Contains uid, resourceVersion, status, etc.

2. **Transform phase** (`crane transform`):
   - KubernetesPlugin analyzed the Deployment
   - Generated patch: `patches/wordpress-demo--apps-v1--Deployment--mysql.patch.yaml`
   - Patch contains "remove" operations for server-managed fields
   - Copied resource to `transform/10_KubernetesPlugin/resources/`
   - Created `kustomization.yaml` to apply the patch

3. **Apply phase** (`crane apply`):
   - Ran `kubectl kustomize transform/10_KubernetesPlugin/`
   - Applied patches to resources
   - Wrote clean resource to `output/output.yaml`
   - Result: clean Deployment ready for target cluster

## Resource Whiteout (Why Are Some Resources Missing?)

You might notice that Pods, ReplicaSets, and Endpoints were **not** included in the transform output. This is intentional!

**Why?** These resources are **generated by controllers**:
- Pods → created by ReplicaSet
- ReplicaSets → created by Deployment
- Endpoints → created by Service controller

When you deploy a Deployment and Service to the target cluster, the controllers will automatically recreate these resources.

**Including them would cause:**
- ❌ Conflicts (Pods with old UIDs)
- ❌ Stale data (ReplicaSets from old rollouts)
- ❌ Confusion (runtime state doesn't transfer)

The KubernetesPlugin automatically **whitelists** controller-managed resources, marking them for exclusion.

## Next Steps

Now that you've completed the basic workflow, explore more advanced scenarios:

- [**Multi-Stage Pipelines**](./03-multistage.md) - Learn how to chain multiple transformation stages
- [**Troubleshooting**](./05-troubleshooting.md) - Common issues and solutions

## Cleanup

Remove the demo resources:

```bash
# Delete the demo namespace
kubectl delete namespace wordpress-demo

# Remove generated directories (optional)
rm -rf export/ transform/ output/
```

## Summary

You've successfully:
- ✅ Deployed a sample application
- ✅ Exported resources with `crane export`
- ✅ Transformed resources with `crane transform`
- ✅ Reviewed the generated patches and kustomization
- ✅ Generated final output with `crane apply`
- ✅ (Optional) Deployed to a target cluster

**Key Takeaways:**
- Transform removes server-managed fields automatically
- Uses standard Kustomize for transformations
- Controller-managed resources are excluded (whiteout)
- Output is ready for GitOps workflows
