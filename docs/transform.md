# Crane Transform - CLI Reference

This document provides a technical reference for the `transform/` directory structure and CLI options.

> ** Looking for tutorials?** See the [Transform Scenarios & Tutorials](./transform-scenarios/README.md) for comprehensive guides and examples.

## Quick Start

After running `crane transform`, you'll see:

```
transform/
└── 10_KubernetesPlugin/
    ├── resources/          # Kubernetes manifests
    ├── patches/            # JSONPatch operations
    └── kustomization.yaml  # Kustomize configuration
```

**Preview output:**
```bash
kubectl kustomize transform/10_KubernetesPlugin/
```

**Apply to cluster:**
```bash
crane apply
kubectl apply -f output/output.yaml
```

## Directory Contents

### resources/
Individual Kubernetes manifest files, named: `Kind_group_version_namespace_name.yaml`

Examples:
- `ConfigMap__v1_default_nginx-config.yaml` (core resource)
- `Deployment_apps_v1_default_wordpress.yaml` (with API group)
- `Namespace__v1_clusterscoped_default.yaml` (cluster-scoped)

### patches/
JSONPatch files for modifying resources, named: `<namespace>--<group>-<version>--<kind>--<name>.patch.yaml`

Example (`default--apps-v1--Deployment--wordpress.patch.yaml`):
```yaml
- op: remove
  path: /metadata/uid
- op: remove
  path: /metadata/resourceVersion
- op: remove
  path: /status
```

### kustomization.yaml
Ties resources and patches together:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- resources/Deployment_apps_v1_default_wordpress.yaml
- resources/Service__v1_default_wordpress.yaml
patches:
- path: patches/default--apps-v1--Deployment--wordpress.patch.yaml
  target:
    group: apps
    version: v1
    kind: Deployment
    name: wordpress
    namespace: default
```

## Multi-Stage Pipelines

Create multiple transformation stages:

```
transform/
├── 10_KubernetesPlugin/     # Base cleanup
├── 20_OpenshiftPlugin/      # Platform conversion
└── 50_CustomEdits/          # Manual customization
```

**Sequential Consistency**: Each stage processes the **fully materialized output** of the previous stage.

### Working Directory Structure

```
transform/
├── 10_KubernetesPlugin/     # Stage artifacts
│   ├── resources/
│   ├── patches/
│   └── kustomization.yaml
└── .work/                   # Intermediate debugging artifacts
    └── 10_KubernetesPlugin/
        ├── input/           # Input snapshot
        └── output/          # Materialized output
```

## Stage Types

### Plugin Stages
**Name ends with `Plugin`** → Runs corresponding plugin

```bash
crane transform --stage 10_KubernetesPlugin   # Uses KubernetesPlugin
crane transform --stage 20_OpenshiftPlugin    # Uses OpenshiftPlugin
```

**Behavior:**
- Auto-regenerates on each run
- Manual edits will be overwritten
- No `--force` needed

### Custom Stages
**Name does NOT end with `Plugin`** → Pass-through for manual editing

```bash
crane transform --stage 50_CustomEdits
```

**Behavior:**
- Resources copied unchanged from previous stage
- Protected from overwrite (requires `--force`)
- Perfect for manual patches and Kustomize features

## CLI Commands

### Running Transform

```bash
# Discover and run all existing stages (or create default)
crane transform

# Run specific stage
crane transform --stage 10_KubernetesPlugin

# Create custom stage
crane transform --stage 50_CustomEdits

# Force overwrite protected stages (WARNING: loses edits)
crane transform --force

# Skip specific plugins
crane transform --skip-plugins OpenshiftPlugin,ImagestreamPlugin

# List available plugins
crane transform list-plugins
```

### Applying Transform

```bash
# Apply all stages sequentially and generate output
crane apply --transform-dir transform --output-dir output
```

**Output structure:**
```
output/
├── output.yaml              # Single multi-document YAML
└── resources/               # Per-resource files by namespace
    ├── default/
    │   ├── Deployment_apps_v1_default_myapp.yaml
    │   └── Service__v1_default_myapp.yaml
    └── kube-system/
        └── Service__v1_kube-system_metrics.yaml
```

## Common Workflows

### Basic Transform
```bash
crane export
crane transform
crane apply
kubectl apply -f output/output.yaml
```

### Multi-Stage Transform
```bash
crane export
crane transform --stage 10_KubernetesPlugin
crane transform --stage 20_OpenshiftPlugin
crane transform --stage 50_CustomEdits
crane apply
```

### Manual Customization
```bash
crane transform --stage 10_KubernetesPlugin
crane transform --stage 50_MyCustomizations

# Edit kustomization.yaml to add Kustomize features
vim transform/50_MyCustomizations/kustomization.yaml

# Add commonLabels, namespace, patches, etc.
crane apply
```

## Resource Naming Format

Resources follow this naming pattern:

```
<Kind>_<group>_<version>_<namespace>_<name>.yaml
```

**Examples:**
- Core resources (no group): `Service__v1_default_myapp.yaml`
- With API group: `Deployment_apps_v1_default_myapp.yaml`
- Cluster-scoped: `Namespace__v1_clusterscoped_default.yaml`

**Underscores:**
- Single `_` separates fields
- Double `__` indicates empty group (core resources)

## CLI Flags Reference

### transform

| Flag | Description | Default |
|------|-------------|---------|
| `--export-dir` | Export directory path | `export` |
| `--transform-dir` | Transform directory path | `transform` |
| `--plugin-dir` | Plugin directory | `~/.local/share/crane/plugins` |
| `--stage` | Run specific stage | (all stages) |
| `--force` | Force overwrite custom stages | `false` |
| `--skip-plugins` | Comma-separated plugin list to skip | - |
| `--optional-flags` | JSON string of plugin-specific flags | - |

### apply

| Flag | Description | Default |
|------|-------------|---------|
| `--transform-dir` | Transform directory path | `transform` |
| `--output-dir` | Output directory path | `output` |

## Advanced: Kustomize Features

Custom stages can use full Kustomize features:

**commonLabels:**
```yaml
commonLabels:
  environment: production
  migrated-with: crane
```

**namespace:**
```yaml
namespace: production
```

**replicas:**
```yaml
replicas:
- name: myapp
  count: 5
```

**configMapGenerator:**
```yaml
configMapGenerator:
- name: app-config
  literals:
  - DATABASE_URL=postgres://db/mydb
  - LOG_LEVEL=info
```

See [Kustomize Documentation](https://kubectl.docs.kubernetes.io/references/kustomize/) for all features.

## Troubleshooting

**Stage not empty error:**
```bash
# Custom stages are protected - use force to overwrite
crane transform --force
```

**Resources missing:**
```bash
# Check kustomization.yaml resources list
cat transform/10_KubernetesPlugin/kustomization.yaml | grep -A 100 "resources:"
```

**Kustomize validation error:**
```bash
# Validate manually
kubectl kustomize transform/10_KubernetesPlugin/
```

**Debug multi-stage:**
```bash
# Check intermediate outputs
ls transform/.work/10_KubernetesPlugin/output/
diff -r transform/.work/10_KubernetesPlugin/output/ \
        transform/.work/20_OpenshiftPlugin/input/
```

## Further Reading

- **[Transform Scenarios & Tutorials](./transform-scenarios/README.md)** - Comprehensive guides and examples
- **[Multi-Stage Kustomize](./kustomize-multistage.md)** - Technical deep-dive into architecture
- **[Kustomize Official Docs](https://kubectl.docs.kubernetes.io/references/kustomize/)** - Learn Kustomize features
- **[Crane README](../README.md)** - Project overview and installation
