# Crane Transform Directory Structure

This document explains the structure of the `transform/` directory created by Crane's multi-stage Kustomize pipeline.

## Quick Start

After running `crane transform`, you'll see:

```
transform/
└── 10_KubernetesPlugin/
    ├── resources/
    │   ├── ConfigMap__v1_default_nginx-config.yaml
    │   ├── Deployment_apps_v1_default_wordpress.yaml
    │   └── Service__v1_default_kubernetes.yaml
    ├── patches/
    │   └── default--apps-v1--Deployment--wordpress.patch.yaml
    └── kustomization.yaml
```

### What's in Each Directory?

- **`resources/`**: Individual Kubernetes manifest files, one per resource
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
vi transform/10_KubernetesPlugin/resources/Deployment_apps_v1_default_wordpress.yaml

# Preview changes
kubectl kustomize transform/10_KubernetesPlugin/

# Apply changes
kubectl apply -k transform/10_KubernetesPlugin/
```

**Important**: 
- **Plugin stages** (ending with `Plugin`): Automatically regenerate on rerun - your manual edits will be overwritten
- **Custom stages** (not ending with `Plugin`): Refuse to overwrite without `--force` flag to protect your edits

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
```

### Applying Transforms

```bash
# Apply all stages sequentially
crane apply --transform-dir transform --output-dir output
```

**Note**: The default behavior applies **all stages** sequentially to ensure each stage output is properly materialized. This maintains sequential consistency across the entire pipeline.

#### Output Structure

`crane apply` creates the following output structure:

```
output/
├── output.yaml                      # Single file with all resources
└── resources/                       # Individual resource files
    ├── default/                     # Organized by namespace
    │   ├── Deployment_apps_v1_default_myapp.yaml
    │   ├── Service__v1_default_myapp.yaml
    │   └── ConfigMap__v1_default_config.yaml
    └── kube-system/
        └── Service__v1_kube-system_metrics.yaml
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
# Note: Custom stage requires --force to overwrite (protects your edits)
crane transform --force
```

**Important**: Custom stages (not ending with `Plugin`) are protected from accidental overwrites. You must use `--force` to regenerate them.

**Best Practice**: Always add custom stages as the **last stage** in your pipeline. This ensures that:
- The `resources/` directory contains the most up-to-date output from all previous stages
- You're editing the final state of resources after all plugin transformations
- Re-running earlier plugin stages won't leave your custom stage with stale data

If you add a custom stage in the middle of the pipeline and later update an earlier stage, you'll need to use `--force` to refresh the custom stage's `resources/` directory with the updated output. **Warning**: Using `--force` will delete the entire stage directory including any manual changes you made to `resources/`, `patches/`, and `kustomization.yaml`.

## Directory Contents Explained

### resources/

Contains individual Kubernetes manifest files, one per resource:

- **Naming format**: `Kind_group_version_namespace_name.yaml`
- **Core resources** (no group): `ConfigMap__v1_default_nginx-config.yaml`
- **Resources with API group**: `Deployment_apps_v1_default_wordpress.yaml`
- **Cluster-scoped resources**: `Namespace__v1_clusterscoped_default.yaml`

Each file contains a single Kubernetes resource (not multi-document YAML).

### patches/

Contains Kustomize patches for modifying resources:

- **Naming**: `<namespace>--<group>-<version>--<kind>--<name>.patch.yaml`
- **Format**: JSON patch
- **Purpose**: Apply plugin transformations

Example patch (`default--apps-v1--Deployment--wordpress.patch.yaml`):

```yaml
- op: remove
  path: /metadata/uid
- op: remove
  path: /metadata/resourceVersion
- op: remove
  path: /status
```

### kustomization.yaml

Kustomize configuration that ties everything together:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
patches:
- path: patches/default--apps-v1--Deployment--wordpress.patch.yaml
  target:
    group: apps
    kind: Deployment
    name: wordpress
    namespace: default
    version: v1
resources:
- resources/ConfigMap__v1_default_nginx-config.yaml
- resources/Deployment_apps_v1_default_wordpress.yaml
- resources/Service__v1_default_kubernetes.yaml

# Whiteout resources are written to resources/ for complete snapshot
# but excluded from active resources list above:
# - resources/Pod__v1_default_wordpress-74b89cc84c-nm9f8.yaml
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

**Note**: Plugin stages (ending with `Plugin`) will be automatically regenerated on next run, overwriting manual edits. For manual customizations, create a custom stage (not ending with `Plugin`).

```bash
# Create a custom stage for manual edits
crane transform --stage 40_CustomEdits

# Edit resources
vi transform/40_CustomEdits/resources/Deployment_apps_v1_default_wordpress.yaml

# Add custom patches
cat > transform/40_CustomEdits/patches/custom-patch.yaml <<EOF
- op: add
  path: /metadata/annotations/custom
  value: "my-value"
EOF

# Update kustomization.yaml to include custom patch
vi transform/40_CustomEdits/kustomization.yaml
```

### 3. Re-run Transform

```bash
# Default: discovers and runs all existing stages
crane transform

# Plugin stages (ending with "Plugin") regenerate automatically
# Custom stages (not ending with "Plugin") require --force to overwrite
crane transform --force

# Run specific plugin stage (regenerates automatically)
crane transform --stage 10_KubernetesPlugin

# Run specific custom stage (requires --force if directory not empty)
crane transform --stage 40_CustomEdits --force
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

# Edit the manual stage (example: edit a specific deployment)
vi transform/40_CustomEdits/resources/Deployment_apps_v1_default_wordpress.yaml

# Run all stages sequentially
# Note: requires --force for custom stages
crane transform --force

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
# Creates: 50_CustomLabels/ as pass-through (resources copied from previous stage)

# 5. Manually add custom labels
cat > transform/50_CustomLabels/patches/add-labels.yaml <<EOF
- op: add
  path: /metadata/labels/environment
  value: production
EOF

# Update kustomization.yaml to include the patch
vi transform/50_CustomLabels/kustomization.yaml

# 6. Run the entire pipeline
# Note: 10_KubernetesPlugin and 20_OpenshiftPlugin regenerate automatically
# 50_CustomLabels will fail unless --force is used (protects manual edits)
crane transform --force

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

### "Stage directory is not empty (use --force to overwrite)"

**Problem**: Crane detects a custom stage already exists and refuses to overwrite to protect manual edits.

**Solution**:
```bash
# Option 1: Use force flag to overwrite
crane transform --force

# Option 2: Check what changed
git diff transform/

# Option 3: Only regenerate plugin stages (they auto-regenerate)
crane transform --stage 10_KubernetesPlugin

# Note: Plugin stages (ending with "Plugin") regenerate automatically without --force
# Custom stages (not ending with "Plugin") require --force to protect manual edits
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
- resources/Deployment_apps_v1_default_wordpress.yaml

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
- resources/Deployment_apps_v1_default_wordpress.yaml

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
- resources/Deployment_apps_v1_default_wordpress.yaml
```

## Further Help

- [Multi-Stage Kustomize Documentation](./kustomize-multistage.md)
- [Kustomize Official Docs](https://kubectl.docs.kubernetes.io/references/kustomize/)
- [Crane GitHub Issues](https://github.com/konveyor/crane/issues)
