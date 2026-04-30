# Example Export - WordPress Demo (stateless)

This directory contains example exported resources from a WordPress + MySQL deployment, used in the [Quickstart Tutorial](../02-quickstart.md).

## How It Was Generated

```bash
# Deploy WordPress demo application
kubectl create namespace wordpress-demo
kubectl apply -n wordpress-demo -f <deployment-yaml>

# Export resources
kubectl config set-context --current --namespace=wordpress-demo
crane export
```

## Using This Example

To use this example export in the tutorial workflow:

```bash
# Start from this directory
cd docs/transform-scenarios

# Transform the exported resources
crane transform

# Apply to generate final manifests
crane apply

# Result: output/output.yaml ready for deployment
```

## Contents

- `resources/wordpress-demo/` - Exported Kubernetes resources
  - Deployments (MySQL, WordPress)
  - Services
  - Secrets
  - PersistentVolumeClaims
  - Generated resources (Pods, ReplicaSets, Endpoints)

See [Quickstart Tutorial](../02-quickstart.md) for detailed walkthrough.
