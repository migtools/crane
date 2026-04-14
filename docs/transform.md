# Crane Transform Directory Structure

This document explains the structure of the `transform/` directory created by Crane's multi-stage Kustomize pipeline.

## Quick Start

After running `crane transform`, you'll see:

```
transform/
└── 10_KubernetesPlugin/
    ├── resources/
    │   ├── deployment.yaml
    │   └── service.yaml
    ├── patches/
    │   └── deployment-myapp-default.yaml
    ├── kustomization.yaml
    └── .crane-metadata.json
```

### What's in Each Directory?

- **`resources/`**: Kubernetes manifests grouped by resource type
- **`patches/`**: Kustomize patches to apply to resources
- **`kustomization.yaml`**: Kustomize configuration file
- **`.crane-metadata.json`**: Metadata for tracking changes (don't edit)

## Working with Transform Output

### Viewing Final Resources

To see what will be deployed:

```bash
kubectl kustomize transform/10_KubernetesPlugin/
```

### Applying to Cluster

```bash
# Option 1: Use crane apply
crane apply --transform-dir transform --output-dir output
kubectl apply -f output/output.yaml

# Option 2: Direct apply
kubectl apply -k transform/10_KubernetesPlugin/
```

### Making Manual Changes

You can edit resources in the `resources/` directory:

```bash
# Edit a deployment
vim transform/10_KubernetesPlugin/resources/deployment.yaml

# Preview changes
kubectl kustomize transform/10_KubernetesPlugin/

# Apply changes
kubectl apply -k transform/10_KubernetesPlugin/
```

**Important**: If you run `crane transform` again, it will detect your changes and refuse to overwrite. Use `--force` to override.

## Multi-Stage Pipelines

For complex transformations, you can create multiple stages:

```
transform/
├── 10_KubernetesPlugin/     # Base Kubernetes transformations
├── 20_OpenshiftPlugin/      # OpenShift-specific changes
└── 30_ImagestreamPlugin/    # ImageStream configurations
```

**Sequential Consistency**: Each stage runs on the **fully applied output** of the previous stage. This means:

- Stage 1 reads from the export directory
- Stage 1's patches are applied, producing materialized output
- Stage 2 reads Stage 1's applied output (not the raw patches)
- Stage 2's patches are applied to the already-transformed resources
- And so on...

This ensures that:
- Structural changes from earlier stages are visible to later stages
- Resources marked as whiteout (deleted) don't appear in subsequent stages
- Each stage sees the actual state of resources, not just patch instructions

### Working Directory Structure

When running multistage transforms, Crane creates a working directory structure for debugging:

```
transform/
├── 10_KubernetesPlugin/     # Stage 1 transform artifacts
│   ├── resources/
│   ├── patches/
│   └── kustomization.yaml
├── 20_OpenshiftPlugin/      # Stage 2 transform artifacts
│   ├── resources/
│   ├── patches/
│   └── kustomization.yaml
└── .work/                   # Intermediate working artifacts
    ├── 10_KubernetesPlugin/
    │   ├── input/           # Stage 1 input snapshot (from export)
    │   └── output/          # Stage 1 materialized output
    └── 20_OpenshiftPlugin/
        ├── input/           # Stage 2 input snapshot (Stage 1 output)
        └── output/          # Stage 2 materialized output
```

The `.work/` directory contains intermediate snapshots useful for debugging multi-stage pipelines.

### Running Multi-Stage Transforms

```bash
# Run all stages in sequence
crane transform --from-stage 10_KubernetesPlugin

# Run specific stage range
crane transform --from-stage 10_KubernetesPlugin --to-stage 20_OpenshiftPlugin

# Run specific stages only
crane transform --stages 10_KubernetesPlugin,30_ImagestreamPlugin
```

To apply all stages:

```bash
crane apply --transform-dir transform --output-dir output
```

This applies only the final stage (30_ImagestreamPlugin), which includes all transformations.

## Directory Contents Explained

### resources/

Contains Kubernetes manifests grouped by resource type:

- **`deployment.yaml`**: All Deployment resources
- **`service.yaml`**: All Service resources
- **`configmap.yaml`**: All ConfigMap resources
- **`route.route.openshift.io.yaml`**: OpenShift Route resources

Each file is a multi-document YAML (separated by `---`).

### patches/

Contains Kustomize patches for modifying resources:

- **Naming**: `<kind>-<name>-<namespace>.yaml`
- **Format**: Strategic merge patch or JSON patch
- **Purpose**: Apply plugin transformations

Example patch (`deployment-myapp-default.yaml`):

```yaml
- op: add
  path: /metadata/labels/transformed
  value: "true"
- op: replace
  path: /spec/replicas
  value: 3
```

### kustomization.yaml

Kustomize configuration that ties everything together:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- resources/deployment.yaml
- resources/service.yaml
patches:
- path: patches/deployment-myapp-default.yaml
  target:
    kind: Deployment
    name: myapp
    namespace: default
```

### .crane-metadata.json

Metadata file for dirty checking (DO NOT EDIT):

```json
{
  "createdAt": "2024-03-26T12:00:00Z",
  "createdBy": "crane-transform",
  "plugin": "transform",
  "craneVersion": "v1.0.0",
  "contentHashes": {
    "resources/deployment.yaml": "sha256:abc123...",
    "patches/deployment-myapp-default.yaml": "sha256:def456..."
  }
}
```

This allows Crane to detect if files have been manually modified.

## Reports

### whiteout-report.yaml

Lists resources that were excluded from the output:

```yaml
- apiVersion: v1
  kind: Pod
  name: temporary-pod
  namespace: default
  requestedBy:
  - cleanup-plugin
```

### ignored-patches-report.yaml

Lists patches that were ignored due to conflicts:

```yaml
- resource:
    apiVersion: apps/v1
    kind: Deployment
    name: myapp
  path: /spec/replicas
  operation: replace
  selectedPlugin: kubernetes-plugin
  ignoredPlugin: custom-plugin
  reason: path-conflict-priority
```

## Common Workflows

### 1. Review Transformations

```bash
# View all resources after transformation
kubectl kustomize transform/10_KubernetesPlugin/

# View specific resource type
kubectl kustomize transform/10_KubernetesPlugin/ | grep -A 20 "kind: Deployment"

# Save to file for review
kubectl kustomize transform/10_KubernetesPlugin/ > review.yaml
```

### 2. Customize After Transform

```bash
# Edit resources
vim transform/10_KubernetesPlugin/resources/deployment.yaml

# Add custom patches
cat > transform/10_KubernetesPlugin/patches/custom-patch.yaml <<EOF
- op: add
  path: /metadata/annotations/custom
  value: "my-value"
EOF

# Update kustomization.yaml to include custom patch
vim transform/10_KubernetesPlugin/kustomization.yaml
```

### 3. Re-run Transform

```bash
# This will fail if you made manual changes
crane transform --export-dir export --transform-dir transform

# Use force to overwrite changes
crane transform --export-dir export --transform-dir transform --force

# Or use a new stage name to preserve changes
crane transform --export-dir export --transform-dir transform --stage-name 15_custom
```

### 4. Chain Stages

```bash
# Create base transformations
crane transform --stage-name 10_KubernetesPlugin --plugin-name KubernetesPlugin

# Create OpenShift transformations on top of base
# (manually ensure 10_KubernetesPlugin exists first)
crane transform --stage-name 20_OpenshiftPlugin --plugin-name OpenshiftPlugin

# Apply final stage
crane apply --transform-dir transform --output-dir output
```

## Git Best Practices

### What to Commit

✅ **Do commit**:
- `resources/*.yaml`
- `patches/*.yaml`
- `kustomization.yaml`
- `whiteout-report.yaml`
- `ignored-patches-report.yaml`

❌ **Don't commit** (add to .gitignore):
- `.crane-metadata.json` (regenerated on each transform)

Example `.gitignore`:

```gitignore
# Crane metadata (regenerated)
transform/**/.crane-metadata.json

# Output directories
output/
```

### Reviewing Changes

```bash
# View staged changes
git diff --staged transform/

# Review resource changes
git diff --staged transform/10_KubernetesPlugin/resources/

# Review patch changes
git diff --staged transform/10_KubernetesPlugin/patches/
```

## Troubleshooting

### "Directory contains user modifications"

**Problem**: Crane detects manual changes and refuses to overwrite.

**Solution**:
```bash
# Option 1: Use force flag
crane transform --force

# Option 2: Check what changed
git diff transform/

# Option 3: Create new stage preserving changes
crane transform --stage-name 20_custom
```

### "kustomization.yaml validation failed"

**Problem**: Invalid Kustomize syntax or missing files.

**Solution**:
```bash
# Validate manually
kubectl kustomize transform/10_KubernetesPlugin/

# Check for missing files
ls -la transform/10_KubernetesPlugin/resources/
ls -la transform/10_KubernetesPlugin/patches/
```

### Resources Missing from Output

**Problem**: Expected resources don't appear in final output.

**Solution**:
```bash
# Check whiteout report
cat transform/10_KubernetesPlugin/whiteout-report.yaml

# Review plugin logs
crane transform --verbose
```

## Advanced: Creating Custom Kustomizations

You can extend generated kustomizations:

### Add ConfigMap Generator

```yaml
# Edit kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- resources/deployment.yaml

configMapGenerator:
- name: app-config
  literals:
  - DATABASE_URL=postgres://localhost
```

### Add Common Labels

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- resources/deployment.yaml

commonLabels:
  app: myapp
  environment: production
```

### Add Namespace

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: production

resources:
- resources/deployment.yaml
```

## Further Help

- [Multi-Stage Kustomize Documentation](./kustomize-multistage.md)
- [Kustomize Official Docs](https://kubectl.docs.kubernetes.io/references/kustomize/)
- [Crane GitHub Issues](https://github.com/konveyor/crane/issues)
