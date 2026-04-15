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
    └── kustomization.yaml
```

### What's in Each Directory?

- **`resources/`**: Kubernetes manifests grouped by resource type
- **`patches/`**: Kustomize patches to apply to resources
- **`kustomization.yaml`**: Kustomize configuration file

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

```text
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

**Important**: The `.work/` directory contains intermediate snapshots that are regenerated on each transform run. These are useful for debugging but should not be committed to version control. Add to your `.gitignore`:

```gitignore
# Crane intermediate artifacts (regenerated on each run)
transform/.work/
```

### Running Multi-Stage Transforms

```bash
# Default: discover and run all existing stages
# If no stages exist, creates default 10_KubernetesPlugin stage
crane transform

# Run specific stage only (creates it if doesn't exist)
crane transform --stage 10_KubernetesPlugin

# Create a new plugin-based stage automatically
# Stage name ending with "Plugin" will use that plugin
crane transform --stage 35_OpenshiftPlugin

# Create a new pass-through stage for manual editing
# Stage name NOT ending with "Plugin" creates empty pass-through
crane transform --stage 40_CustomManualEdits

# Run from a specific stage onwards
crane transform --from-stage 10_KubernetesPlugin

# Run specific stage range
crane transform --from-stage 10_KubernetesPlugin --to-stage 20_OpenshiftPlugin

# Run specific stages only
crane transform --stages 10_KubernetesPlugin,30_ImagestreamPlugin
```

### Applying Transforms

```bash
# Default: apply all stages sequentially
crane apply --transform-dir transform --output-dir output

# Apply specific stage only
crane apply --stage 10_KubernetesPlugin

# Apply specific stage range
crane apply --from-stage 10_KubernetesPlugin --to-stage 20_OpenshiftPlugin
```

**Note**: The default behavior applies **all stages** sequentially to ensure each stage output is properly materialized. This maintains sequential consistency across the entire pipeline.

#### Output Structure

`crane apply` creates the following output structure:

```
output/
├── output.yaml                      # Single file with all resources
└── resources/                       # Individual resource files
    ├── default/                     # Organized by namespace
    │   ├── Deployment_default_myapp.yaml
    │   ├── Service_default_myapp.yaml
    │   └── ConfigMap_default_config.yaml
    └── kube-system/
        └── Service_kube-system_metrics.yaml
```

- **`output.yaml`**: All resources in a single multi-document YAML file (ready for `kubectl apply -f`)
- **`resources/<namespace>/`**: Individual resource files organized by namespace for easier review and selective application

## Automatic Stage Creation

Crane automatically creates new stages when you reference them with `--stage`. The stage name determines whether a plugin will be used or if it's a pass-through stage for manual editing.

### Stage Naming Convention

Stage names follow the pattern `<number>_<Name>`:

**Plugin-based stages (name ends with "Plugin"):**
- `10_KubernetesPlugin` → uses plugin "KubernetesPlugin"
- `20_OpenshiftPlugin` → uses plugin "OpenshiftPlugin"
- `35_CustomPlugin` → uses plugin "CustomPlugin"

**Pass-through stages (name does NOT end with "Plugin"):**
- `30_CustomEdits` → no plugin, resources pass through unchanged
- `40_ManualChanges` → no plugin, ready for manual editing
- `50_Tweaks` → no plugin, resources unchanged

### Plugin-Based Stages

Stage names ending with `Plugin` **must** have a corresponding plugin installed. Crane extracts the plugin name and runs it:

```bash
# Creates a stage using OpenshiftPlugin
crane transform --stage 20_OpenshiftPlugin

# Output:
# - resources/ (from previous stage or export)
# - patches/ (generated by OpenshiftPlugin)
# - kustomization.yaml
```

**If the plugin doesn't exist, you'll get an error:**

```bash
crane transform --stage 20_NonexistentPlugin
# Error: stage 20_NonexistentPlugin requires plugin 'NonexistentPlugin' 
#        but it was not found (available plugins: KubernetesPlugin, NamespaceCleanup)
```

### Pass-Through Stages

Stage names **not** ending with `Plugin` create pass-through stages where resources are copied unchanged, ready for manual editing:

```bash
# Creates a pass-through stage for manual editing
crane transform --stage 30_CustomEdits

# Output:
# - resources/ (copied unchanged from previous stage)
# - patches/ (empty - ready for your custom patches)
# - kustomization.yaml
```

You can then manually add custom patches:

```bash
# Add custom patch
cat > transform/30_CustomEdits/patches/add-labels.yaml <<EOF
- op: add
  path: /metadata/labels/environment
  value: production
EOF

# Update kustomization.yaml to reference the patch
# Then run transform to apply it
crane transform
```

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
# Default: discovers and runs all existing stages
crane transform

# This will fail if you made manual changes - use force to overwrite
crane transform --force

# Run specific stage only (if you want to regenerate just one stage)
crane transform --stage 10_KubernetesPlugin --force
```

### 4. Working with Multiple Stages

```bash
# First run creates default 10_KubernetesPlugin stage
crane transform

# Automatically add more plugin stages
crane transform --stage 20_OpenshiftPlugin
crane transform --stage 30_ImagestreamPlugin

# Add a manual editing stage
crane transform --stage 40_CustomEdits

# Edit the manual stage
vim transform/40_CustomEdits/resources/deployment.yaml

# Run all stages sequentially
crane transform

# Apply all stages
crane apply --transform-dir transform --output-dir output
```

### 5. Example: Building a Multi-Stage Pipeline

```bash
# 1. Export from source cluster
crane export --context source-cluster

# 2. Create initial Kubernetes transformations
crane transform
# Creates: 10_KubernetesPlugin/

# 3. Add OpenShift-specific transformations
crane transform --stage 20_OpenshiftPlugin
# Creates: 20_OpenshiftPlugin/ using OpenshiftPlugin

# 4. Add a manual customization stage
crane transform --stage 50_CustomLabels
# Creates: 50_CustomLabels/ as empty pass-through

# 5. Manually add custom labels
cat > transform/50_CustomLabels/patches/add-labels.yaml <<EOF
- op: add
  path: /metadata/labels/environment
  value: production
EOF

# Update kustomization.yaml to include the patch
vim transform/50_CustomLabels/kustomization.yaml

# 6. Run the entire pipeline
crane transform

# 7. Verify the output
kubectl kustomize transform/50_CustomLabels/

# 8. Apply to target cluster
crane apply
kubectl apply -f output/output.yaml
```

## Git Best Practices

### What to Commit

✅ **Do commit**:
- `transform/*/resources/*.yaml`
- `transform/*/patches/*.yaml`
- `transform/*/kustomization.yaml`

❌ **Don't commit** (add to .gitignore):
- `transform/.work/` (intermediate artifacts, regenerated on each transform)
- `output/` (generated by crane apply)

Example `.gitignore`:

```gitignore
# Crane intermediate artifacts (regenerated)
transform/.work/

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
# Check input snapshot to see what was loaded
ls -la transform/.work/10_KubernetesPlugin/input/

# Compare input vs output to see what was filtered
diff -r transform/.work/10_KubernetesPlugin/input/ transform/.work/10_KubernetesPlugin/output/

# Review plugin logs for whiteout/filtering
crane transform --debug
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
