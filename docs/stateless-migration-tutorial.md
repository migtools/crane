# Stateless Migration Tutorial

This tutorial walks through migrating a stateless application from a source cluster to a target cluster using the full Crane pipeline.

## Scenario

You have a web application running in your source cluster with Deployments, Services, ConfigMaps, and Secrets. You need to migrate it to a new cluster, potentially with modifications (different namespace, cloud-specific adjustments, etc.).

## Prerequisites

- [Crane installed](./installation.md)
- `kubectl` configured with access to both source and target clusters
- Source cluster has a running application to migrate

## Step 1: Prepare

Identify the namespace and verify the application is running:

```bash
# Set your source context
export SOURCE_CTX=source-cluster
export TARGET_CTX=target-cluster
export APP_NS=my-app

# Verify the application
kubectl --context $SOURCE_CTX -n $APP_NS get all
```

## Step 2: Export

Export all resources from the source namespace:

```bash
crane export -n $APP_NS --context $SOURCE_CTX --export-dir migration/export
```

Review what was exported:

```bash
ls migration/export/resources/$APP_NS/
```

Check for any export failures:

```bash
ls migration/export/failures/$APP_NS/ 2>/dev/null || echo "No failures"
```

## Step 3: Transform

Run the default Kubernetes plugin to clean server-managed fields:

```bash
crane transform --export-dir migration/export --transform-dir migration/transform
```

This creates a single stage (`10_KubernetesPlugin`) that removes fields like `metadata.uid`, `metadata.resourceVersion`, and `status`.

### Adding Additional Stages (Optional)

If migrating between different platforms (e.g., OpenShift to vanilla Kubernetes), add more stages:

```bash
# Add OpenShift-specific transformations
crane transform --transform-dir migration/transform --stage 20_OpenshiftPlugin

# Add a manual customization stage
crane transform --transform-dir migration/transform --stage 30_CustomEdits
```

You can then manually edit resources in the custom stage:

```bash
vi migration/transform/30_CustomEdits/resources/<resource-file>.yaml
```

### Preview Changes

Review the final transformed output before applying:

```bash
kubectl kustomize migration/transform/10_KubernetesPlugin/
```

## Step 4: Apply

Generate the final clean manifests:

```bash
crane apply --transform-dir migration/transform --output-dir migration/output
```

Review the output:

```bash
# View the combined output
cat migration/output/output.yaml

# Or browse individual files
ls migration/output/resources/$APP_NS/
```

## Step 5: Validate

Check that all resource types are supported by the target cluster:

```bash
crane validate --input-dir migration/output --context $TARGET_CTX
```

If validation fails, review the report and adjust your transform stages.

## Step 6: Deploy

Apply the manifests to the target cluster:

```bash
# Create the namespace if it doesn't exist
kubectl --context $TARGET_CTX create namespace $APP_NS --dry-run=client -o yaml | \
  kubectl --context $TARGET_CTX apply -f -

# Deploy the application
kubectl --context $TARGET_CTX apply -f migration/output/output.yaml
```

Verify the deployment:

```bash
kubectl --context $TARGET_CTX -n $APP_NS get all
kubectl --context $TARGET_CTX -n $APP_NS get pods -w
```

## Step 7: Version Control (Optional)

For GitOps workflows, commit the migration artifacts:

```bash
cd migration
git init
echo "transform/.work/" >> .gitignore
echo "output/" >> .gitignore
git add .
git commit -m "feat: migrate $APP_NS from source to target cluster"
```

## Troubleshooting

### Export finds no resources

- Verify the namespace exists: `kubectl --context $SOURCE_CTX get ns $APP_NS`
- Check RBAC permissions: `kubectl --context $SOURCE_CTX auth can-i list deployments -n $APP_NS`

### Transform fails with plugin errors

- List available plugins: `crane transform list-plugins`
- Skip problematic plugins: `crane transform --skip-plugins problematic-plugin`

### Apply fails with kustomize errors

- Validate manually: `kubectl kustomize migration/transform/10_KubernetesPlugin/`
- Check for missing resource files in the `resources/` directory

### Validation reports incompatible resources

- Install required CRDs on the target cluster
- Add a transform stage to convert resources to supported API versions

## Next Steps

- [Stateful Migration Tutorial](./stateful-migration-tutorial.md) — Migrating applications with persistent volumes
- [Multi-Stage Pipeline](./multistage-pipeline.md) — Advanced multi-stage transformations
- [Troubleshooting](./troubleshooting.md) — Common issues and solutions
