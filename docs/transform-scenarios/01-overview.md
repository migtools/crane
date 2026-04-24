# Crane Migration - Overview

## What is Crane Transform?

Crane Transform is the second phase in Crane's Kubernetes migration workflow. It takes exported resources from a source cluster and prepares them for deployment to a target cluster by:

1. **Cleaning resources** - Removing cluster-specific metadata that would cause conflicts
2. **Applying transformations** - Modifying resources to work in the target environment
3. **Organizing artifacts** - Creating a Kustomize-based directory structure ready for GitOps

## The Migration Workflow

```
┌──────────────┐     ┌───────────────┐     ┌─────────────┐     ┌──────────┐     ┌──────────────┐     ┌──────────────┐
│   Source     │────▶│ crane export  │────▶│ crane       │────▶│ crane    │────▶│   kubectl    │────▶│   Target     │
│   Cluster    │     │               │     │ transform   │     │ apply    │     │   apply      │     │   Cluster    │
└──────────────┘     └───────────────┘     └─────────────┘     └──────────┘     └──────────────┘     └──────────────┘
                             │                     │                   │
                             ▼                     ▼                   ▼
                     export/resources/     transform/stages/      output/
                     (raw resources)       (cleaned + patches)    (final YAML)
```

### Phase 1: Export
```bash
crane export
```
- Captures live resources from source cluster (uses current namespace context)
- Saves to `export/resources/` directory
- Includes ALL metadata (UIDs, resourceVersions, status, etc.)

### Phase 2: Transform (This Guide)
```bash
crane transform
```
- Removes cluster-specific metadata
- Applies environment-specific transformations
- Creates Kustomize directory structure
- Supports multi-stage pipelines

### Phase 3: Apply
```bash
crane apply
kubectl apply -f output/output.yaml
```
- Builds final manifests using Kustomize
- Deploys to target cluster

## Why Do We Need Transform?

Exported resources contain **server-managed fields** that must be removed:

```yaml
# Exported resource (from source cluster)
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
  uid: 8fb75dcd-68b2-4939-bfb9-1c8241a7b146        # ← Cluster-specific
  resourceVersion: "3213488"                        # ← Will conflict
  creationTimestamp: "2024-01-15T10:30:00Z"        # ← Old timestamp
  managedFields: [...]                              # ← Server-managed
spec:
  replicas: 3
  # ... application config ...
status:                                              # ← Runtime state
  availableReplicas: 3
  # ... current status ...
```

**Attempting to apply this directly to a target cluster will fail** because:
- `uid` conflicts with target cluster's UID assignment
- `resourceVersion` is etcd-specific to the source cluster
- `status` section is read-only and server-managed

**After transform:**
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
  namespace: default
  # ✓ Clean metadata - no conflicts
spec:
  replicas: 3
  # ... application config preserved ...
# ✓ status removed
```

## Key Concepts

### Plugins

**Plugins** analyze resources and generate transformations:

- **Built-in KubernetesPlugin**: Removes server-managed fields (uid, resourceVersion, status, etc.)
- **OpenshiftPlugin**: Handles OpenShift-specific resources (Routes, ImageStreams, etc.)
- **Custom plugins**: Your own transformations via crane-lib

Plugins don't modify resources directly - they generate **patches** that Kustomize applies.

### Stages

A **stage** is a directory containing:
- `resources/` - Kubernetes manifests (one file per resource or grouped by type)
- `patches/` - Kustomize patches (JSONPatch operations)
- `kustomization.yaml` - Kustomize configuration

**Naming convention**: `<priority>_<Name>`
- `10_KubernetesPlugin` - Plugin-backed stage
- `50_CustomEdits` - Pass-through stage for manual editing

### Multi-Stage Pipeline

Complex migrations often need **multiple transformation phases**:

```
export/
  └─▶ 10_KubernetesPlugin/    (removes server metadata)
        └─▶ 20_OpenshiftPlugin/  (converts OpenShift resources)
              └─▶ 30_CustomEdits/  (manual tweaks)
                    └─▶ output/
```

**Sequential Consistency**: Each stage processes the **fully materialized output** of the previous stage, not raw patches.

### Kustomize-Based Architecture

Transform generates standard **Kustomize** layouts:

**Benefits:**
- Industry-standard tool (built into kubectl)
- Declarative transformations (patches are GitOps-friendly)
- Supports overlays, components, and advanced features
- Human-readable diffs in Git

**Example structure:**
```
transform/10_KubernetesPlugin/
├── resources/
│   ├── Deployment_apps_v1_default_myapp.yaml
│   └── Service__v1_default_myapp.yaml
├── patches/
│   └── default--apps-v1--Deployment--myapp.patch.yaml
└── kustomization.yaml
```

## What Transform Does (Behind the Scenes)

When you run `crane transform`:

1. **Load Input**: Reads resources from `export/` (or previous stage output)
2. **Run Plugins**: Each plugin analyzes resources and generates JSONPatch operations
3. **Write Artifacts**:
   - Copies resources to `resources/`
   - Saves patches to `patches/`
   - Generates `kustomization.yaml`
4. **Apply Transforms**: Runs `kubectl kustomize` to produce cleaned output
5. **Save Output**: Writes to `.work/<stage>/output/` for next stage

## Common Use Cases

### Use Case 1: Simple Migration (Single Cluster Type)
**Scenario**: Migrating between two vanilla Kubernetes clusters

```bash
crane export
crane transform  # Uses default 10_KubernetesPlugin
crane apply
kubectl apply -f output/output.yaml
```

### Use Case 2: Cross-Platform Migration
**Scenario**: Migrating from OpenShift to vanilla Kubernetes

```bash
crane export
crane transform --stage 10_KubernetesPlugin  # Clean resources
crane transform --stage 20_OpenshiftPlugin   # Convert OpenShift resources
crane apply
```

### Use Case 3: GitOps Workflow
**Scenario**: Preparing resources for Git repository

```bash
crane export
crane transform
crane apply
# Review and commit transform/ directory
git add output/resources/
git commit -m "Add transformed manifests"
git push
# ArgoCD/Flux picks up changes
```

### Use Case 4: Manual Customization
**Scenario**: Need to hand-edit some resources

```bash
crane export
crane transform --stage 10_KubernetesPlugin
crane transform --stage 50_CustomEdits  # Creates pass-through stage
# Edit files in transform/50_CustomEdits/resources/ with kustomize features
vim transform/50_CustomEdits/kustomize.yaml
crane apply
```

## Directory Structure Reference

After running `crane transform`, your directory looks like:

```
.
├── export/                          # Phase 1: Exported resources
│   └── resources/
│       └── default/
│           ├── Deployment_apps_v1_default_myapp.yaml
│           └── Service__v1_default_myapp.yaml
│
├── transform/                       # Phase 2: Transform stages
│   ├── 10_KubernetesPlugin/
│   │   ├── resources/              # Input resources
│   │   ├── patches/                # Generated patches
│   │   └── kustomization.yaml      # Kustomize config
│   └── .work/                      # Intermediate artifacts (debugging)
│       └── 10_KubernetesPlugin/
│           ├── input/              # Input snapshot
│           └── output/             # Materialized output
│
└── output/                          # Phase 3: Final manifests
    ├── output.yaml                 # Single-file output
    └── resources/                  # Per-resource files
        └── default/
            ├── Deployment_apps_v1_default_myapp.yaml
            └── Service__v1_default_myapp.yaml
```

## Next Steps

- [**Quickstart Tutorial**](./02-quickstart.md) - Hands-on walkthrough
- [**Multi-Stage Pipelines**](./03-multistage.md) - Advanced transformation workflows
- [**Troubleshooting**](./05-troubleshooting.md) - Common issues and solutions

## Reference Documentation

- [Transform CLI Reference](../transform.md) - Detailed directory structure and CLI options
- [Multi-Stage Kustomize](../kustomize-multistage.md) - Technical deep-dive
- [Crane README](../../README.md) - Project overview
