# Multi-Stage Kustomize Transform Pipeline

This document describes the multi-stage Kustomize transform pipeline feature for Crane, which replaces the previous JSONPatch file-per-resource workflow with a more flexible, scalable approach.

## Overview

The multi-stage pipeline allows transformations to be organized into sequential stages, where each stage can apply a different set of plugins to resources. Stages are processed in order based on their priority, and the output of one stage becomes the input for the next.

## Key Concepts

### Stage Directory Structure

Each stage is a directory following the naming convention `<priority>_<plugin-name>`:

```
transform/
├── 10_kubernetes/
│   ├── resources/
│   │   ├── deployment.yaml          # Grouped by resource type
│   │   ├── service.yaml
│   │   └── configmap.yaml
│   ├── patches/
│   │   ├── deployment-myapp-default.yaml
│   │   └── service-myapp-default.yaml
│   ├── kustomization.yaml            # Generated Kustomize file
│   ├── whiteout-report.yaml          # Resources excluded from output
│   ├── ignored-patches-report.yaml   # Patches discarded due to conflicts
│   └── .crane-metadata.json          # Stage metadata with content hashes
├── 20_openshift/
│   └── ...
└── 30_imagestream/
    └── ...
```

### Resource Grouping

Resources are grouped by type (kind + API group) into multi-document YAML files:
- Core resources: `deployment.yaml`, `service.yaml`, `pod.yaml`
- Non-core resources: `route.route.openshift.io.yaml`, `imagestream.image.openshift.io.yaml`

### Kustomization File

Each stage contains a `kustomization.yaml` that references:
- **resources**: List of resource files from the `resources/` directory
- **patches**: Strategic merge patches or JSON patches with target selectors

Example:
```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- resources/deployment.yaml
- resources/service.yaml
patches:
- path: patches/deployment-myapp-default.yaml
  target:
    group: apps
    version: v1
    kind: Deployment
    name: myapp
    namespace: default
```

### Dirty Check

Each stage includes `.crane-metadata.json` with SHA256 hashes of all files. This enables:
- Detection of user modifications
- Prevention of accidental overwrites
- Tracking of transform provenance

## CLI Usage

### Transform Command

#### Single Stage Mode (Default)

Create a single transform stage:

```bash
crane transform \
  --export-dir export \
  --transform-dir transform \
  --stage-name 10_transform \
  --plugin-name transform
```

#### Multi-Stage Mode

Execute specific stages:

```bash
# Run a specific stage
crane transform --stage 20_openshift

# Run from a stage onwards
crane transform --from-stage 20_openshift

# Run up to a specific stage
crane transform --to-stage 30_imagestream

# Run specific stages
crane transform --stages 10_kubernetes,30_imagestream
```

#### Force Overwrite

Override dirty check protection:

```bash
crane transform --force --stage-name 10_kubernetes
```

### Apply Command

#### Apply Final Stage (Default)

Apply only the last stage in the pipeline:

```bash
crane apply \
  --transform-dir transform \
  --output-dir output
```

This builds the final stage using `kubectl kustomize build` and writes the result to `output/output.yaml`.

#### Apply Specific Stages

```bash
# Apply a specific stage
crane apply --stage 20_openshift

# Apply from a stage onwards
crane apply --from-stage 20_openshift

# Apply up to a specific stage
crane apply --to-stage 30_imagestream

# Apply specific stages
crane apply --stages 10_kubernetes,30_imagestream
```

#### Validation

Preflight validation is enabled by default:

```bash
# Run validation (default: enabled)
crane apply --validate

# Skip validation
crane apply --validate=false
```

Validation checks:
- Stage directory structure
- kustomization.yaml syntax
- Resource file existence
- Patch file references
- Stage chaining correctness

## Priority Assignment

### Auto-Assignment

Plugin priorities are automatically assigned from stage directory names:

```go
// Stage directories
10_kubernetes    → plugin "kubernetes" gets priority 10
20_openshift     → plugin "openshift" gets priority 20
30_imagestream   → plugin "imagestream" gets priority 30
```

### Manual Assignment

Override auto-assigned priorities:

```bash
crane transform \
  --plugin-priorities kubernetes:5,openshift:15,imagestream:25
```

### Recommended Priority Order

The system provides heuristic-based recommendations:

| Plugin Type        | Recommended Priority | Keywords                    |
|-------------------|--------------------|------------------------------|
| Kubernetes Core   | 10                 | kubernetes, k8s, core        |
| OpenShift         | 20                 | openshift, ocp               |
| Namespace/Project | 30                 | namespace, project           |
| Security          | 40                 | security, scc, psp           |
| Network           | 50                 | network, route, ingress      |
| Storage           | 60                 | storage, pvc, pv             |
| Image             | 70                 | image, imagestream, registry |
| Build             | 80                 | build, buildconfig           |
| Custom            | 90                 | custom, app, application     |

## Stage Chaining

Stages are chained automatically based on priority order:

```
export/ → 10_kubernetes/ → 20_openshift/ → 30_imagestream/ → output/
```

Each stage:
1. Reads input resources (from export or previous stage)
2. Applies transformations via plugins
3. Writes output to its stage directory
4. Next stage uses this output as input

## Workflow Examples

### Example 1: Simple Transform and Apply

```bash
# Export resources from source cluster
crane export --kubeconfig source.yaml --export-dir export

# Transform resources (single stage)
crane transform --export-dir export --transform-dir transform

# Apply transformations
crane apply --transform-dir transform --output-dir output

# Deploy to target cluster
kubectl apply -f output/output.yaml
```

### Example 2: Multi-Stage Pipeline

```bash
# Export resources
crane export --kubeconfig source.yaml --export-dir export

# Create Kubernetes base transformations
crane transform \
  --export-dir export \
  --transform-dir transform \
  --stage-name 10_kubernetes \
  --plugin-name kubernetes

# Create OpenShift-specific transformations
crane transform \
  --transform-dir transform \
  --stage-name 20_openshift \
  --plugin-name openshift

# Create ImageStream transformations
crane transform \
  --transform-dir transform \
  --stage-name 30_imagestream \
  --plugin-name imagestream

# Apply final stage
crane apply --transform-dir transform --output-dir output
```

### Example 3: Iterative Development

```bash
# Initial transform
crane transform --export-dir export --transform-dir transform

# Make manual edits to resources in transform/10_transform/resources/
# Edit deployment.yaml to add annotations, etc.

# Try to re-run transform (will fail due to dirty check)
crane transform --export-dir export --transform-dir transform
# Error: contains user modifications

# Force overwrite if needed
crane transform --export-dir export --transform-dir transform --force

# Or preserve changes by using a different stage name
crane transform --stage-name 20_custom
```

## Migration from JSONPatch Workflow

### Old Workflow

```
transform/
├── namespace/
│   └── default/
│       └── deployment/
│           └── myapp.json          # JSONPatch per resource
```

### New Workflow

```
transform/
└── 10_transform/
    ├── resources/
    │   └── deployment.yaml         # Grouped by type
    ├── patches/
    │   └── deployment-myapp-default.yaml
    └── kustomization.yaml
```

### Benefits

1. **Reduced File Count**: Resources grouped by type instead of one file per resource
2. **Standard Format**: Uses Kustomize, a widely adopted tool
3. **Stage Chaining**: Supports multi-stage pipelines for complex transformations
4. **Better Diff**: Deterministic ordering produces stable Git diffs
5. **Dirty Check**: Prevents accidental overwrites of user modifications

## Advanced Features

### Stage Validation

Validate stages before applying:

```go
import "github.com/konveyor/crane/internal/apply"

results, err := apply.ValidateAllStages(transformDir)
for _, result := range results {
    if !result.IsValid {
        fmt.Printf("Stage %s has errors:\n", result.StageName)
        for _, err := range result.Errors {
            fmt.Printf("  - %s\n", err)
        }
    }
}
```

### Custom Stage Naming

Suggest stage names based on existing stages:

```go
import "github.com/konveyor/crane/internal/transform"

stageName, err := transform.SuggestStageName(transformDir, "my-plugin")
// Returns: "15_my-plugin" if gap exists, or "40_my-plugin" if appending
```

### Priority Conflict Detection

```go
assignments, err := transform.GetPriorityAssignments(transformDir)
err = transform.ValidatePriorityAssignments(assignments)
if err != nil {
    // Handle priority conflicts
}
```

## Troubleshooting

### Issue: Transform fails with "contains user modifications"

**Cause**: Stage directory has been manually edited after creation.

**Solution**:
1. Use `--force` to overwrite changes
2. Use a different `--stage-name` to preserve changes
3. Commit changes to Git before re-running transform

### Issue: Apply fails with "kustomization.yaml validation failed"

**Cause**: Invalid Kustomize syntax or missing resources.

**Solution**:
1. Check kustomization.yaml syntax
2. Verify all resource files exist in resources/
3. Run `kubectl kustomize build transform/STAGE/` manually to see detailed error

### Issue: Resources not appearing in output

**Cause**: Resources may be whiteout (excluded) by plugins.

**Solution**:
1. Check `whiteout-report.yaml` in stage directory
2. Review plugin configuration
3. Check plugin logs for whiteout decisions

### Issue: Patches not being applied

**Cause**: Patch file or target selector may be incorrect.

**Solution**:
1. Verify patch file exists in patches/
2. Check target selector matches resource metadata
3. Review `ignored-patches-report.yaml` for conflicts

## Best Practices

1. **Stage Naming**: Use descriptive names that indicate the transformation purpose
   - Good: `10_kubernetes-base`, `20_openshift-routes`, `30_security-context`
   - Bad: `10_stage1`, `20_stage2`

2. **Priority Spacing**: Leave gaps (10, 20, 30) to allow insertion of new stages

3. **Version Control**: Commit transform directories to Git to track changes

4. **Validation**: Always run validation before applying to production

5. **Incremental Changes**: Use separate stages for different concerns (security, networking, storage)

6. **Documentation**: Include README.md in transform directory explaining pipeline purpose

## API Reference

### Transform Package

```go
// Stage discovery
stages, err := transform.DiscoverStages(transformDir)

// Stage filtering
selector := transform.StageSelector{
    FromStage: "20_openshift",
    ToStage:   "30_imagestream",
}
filtered := transform.FilterStages(stages, selector)

// Priority assignment
priorities := transform.AutoAssignPriorities(stages)
merged := transform.MergePriorities(userPriorities, priorities)

// Dirty check
dirty, err := transform.IsDirectoryDirty(stageDir)
err = transform.EnsureCleanDirectory(stageDir, force)
```

### Apply Package

```go
// Kustomize apply
applier := &apply.KustomizeApplier{
    Log:          logger,
    TransformDir: transformDir,
    OutputDir:    outputDir,
}

err = applier.ApplyFinalStage()

// Validation
result, err := apply.ValidateStage(transformDir, stageName)
err = apply.ValidatePipeline(transformDir)
```

## Further Reading

- [Kustomize Documentation](https://kubectl.docs.kubernetes.io/references/kustomize/)
- [Crane Plugin Development](./plugin-development.md)
- [Transform Architecture](./architecture.md)
