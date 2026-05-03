# Quickstart Tutorial

Migrate a sample application between Kubernetes clusters in under 10 minutes.

## Prerequisites

- [Crane installed](./installation.md)
- `kubectl` installed and configured
- Access to a source Kubernetes cluster
- (Optional) Access to a target cluster

## Step 1: Deploy a Sample Application

Deploy the guestbook application to your source cluster:

```bash
kubectl create namespace guestbook
kubectl --namespace guestbook apply -k github.com/konveyor/crane-runner/examples/resources/guestbook
```

Verify the deployment:

```bash
kubectl -n guestbook get all
```

## Step 2: Export Resources

Export all resources from the `guestbook` namespace:

```bash
crane export -n guestbook
```

This creates an `export/` directory containing the raw YAML manifests:

```text
export/
└── resources/
    └── guestbook/
        ├── Deployment_apps_v1_guestbook_frontend.yaml
        ├── Service__v1_guestbook_frontend.yaml
        ├── Secret__v1_guestbook_builder-dockercfg-xxxxx.yaml
        └── ...
```

Review an exported resource:

```bash
cat export/resources/guestbook/Deployment_apps_v1_guestbook_frontend.yaml
```

Notice the server-managed fields like `metadata.uid`, `metadata.resourceVersion`, and `status` — these need to be removed before applying to a new cluster.

## Step 3: Transform Resources

Run the transform step to clean resources using plugins:

```bash
crane transform
```

This creates a `transform/` directory with a Kustomize layout:

```text
transform/
└── 10_KubernetesPlugin/
    ├── resources/          # Original exported resources
    ├── patches/            # JSONPatch operations to clean resources
    └── kustomization.yaml  # Kustomize configuration
```

The built-in Kubernetes plugin generates patches that remove server-managed fields (`metadata.uid`, `metadata.resourceVersion`, `metadata.creationTimestamp`, `status`, etc.).

Preview what the transformed output looks like:

```bash
kubectl kustomize transform/10_KubernetesPlugin/
```

## Step 4: Apply Transformations

Generate the final clean manifests:

```bash
crane apply
```

This produces the `output/` directory:

```text
output/
├── output.yaml       # All resources in a single file
└── resources/        # Individual resource files by namespace
    └── guestbook/
        └── ...
```

Review the final output:

```bash
cat output/output.yaml
```

The server-managed fields have been removed — these manifests are ready for deployment.

## Step 5: Deploy to Target Cluster

Apply the clean manifests to your target cluster:

```bash
# Switch to target cluster context (if different)
kubectl config use-context target-cluster

# Apply the manifests
kubectl apply -f output/output.yaml
```

## Step 6: Validate (Optional)

If you have access to the target cluster, validate compatibility of the generated manifests:

```bash
crane validate --context target-cluster
```

This checks that all API versions and resource types in your manifests are supported by the target cluster.

## What's Next?

- [Stateless Migration Tutorial](./stateless-migration-tutorial.md) — A more detailed real-world example
- [Stateful Migration Tutorial](./stateful-migration-tutorial.md) — Migrating applications with persistent volumes
- [Multi-Stage Pipeline](./multistage-pipeline.md) — Using multiple transform stages for complex migrations
- [Command Reference](./README.md#command-reference) — Detailed documentation for each command
