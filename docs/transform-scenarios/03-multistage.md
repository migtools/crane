# Multi-Stage Transform Pipelines

This guide covers advanced multi-stage transformation workflows, showing you when and how to use multiple stages to handle complex migration scenarios.

## What Are Multi-Stage Pipelines?

A multi-stage pipeline processes resources through a **sequence of transformation stages**, where each stage:

1. Reads the **fully materialized output** from the previous stage
2. Applies its own transformations
3. Writes output for the next stage

```
export/              Stage 1             Stage 2             Stage 3
resources/    ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
              │ 10_Kubernetes   │  │ 20_Openshift    │  │ 50_CustomEdits  │
─────────────▶│ Plugin          │─▶│ Plugin          │─▶│                 │─▶ output/
              │                 │  │                 │  │                 │
              │ - Clean metadata│  │ - Convert Routes│  │ - Manual tweaks │
              └─────────────────┘  └─────────────────┘  └─────────────────┘
```

## When to Use Multi-Stage Pipelines

### Single Stage Is Sufficient When:
- ✅ Migrating between similar Kubernetes clusters
- ✅ Only need basic cleanup (remove UIDs, status, etc.)
- ✅ No platform-specific resources (OpenShift, etc.)
- ✅ No manual customization needed

### Multi-Stage Is Better When:
- ✅ Cross-platform migration (OpenShift to Kubernetes, or vice versa)
- ✅ Multiple transformation concerns (cleanup, conversion, customization)
- ✅ Need to inspect intermediate results
- ✅ Want to separate automated and manual changes
- ✅ Complex transformations that benefit from separation of concerns

## Stage Types

### Plugin-Based Stages (Auto-Regenerate)

Stage names ending with `Plugin` use a corresponding plugin:

```bash
crane transform --stage 10_KubernetesPlugin   # Uses KubernetesPlugin
crane transform --stage 20_OpenshiftPlugin    # Uses OpenshiftPlugin
```

**Behavior:**
- Automatically run plugin on each transform
- Always regenerate (no `--force` needed)
- Cannot manually edit (changes will be overwritten)

**Best for:**
- Automated transformations
- Repeatable processing
- Plugin-driven cleanup/conversion

### Pass-Through Stages (Manual Edit Protection)

Stage names **NOT** ending with `Plugin` create pass-through stages:

```bash
crane transform --stage 50_CustomEdits    # No plugin - pass-through
crane transform --stage 90_FinalTweaks    # No plugin - pass-through
```

**Behavior:**
- Resources copied unchanged from previous stage
- No patches generated automatically
- Protected from accidental overwrite (requires `--force`)
- Perfect for manual editing

**Best for:**
- Manual customizations
- Hand-crafted patches
- Environment-specific changes

## Example: Basic Two-Stage Pipeline

**Scenario:** Cleaning resources and then adding custom labels

### Step 1: Export Resources

```bash
crane export
```

### Step 2: Create Cleanup Stage

```bash
crane transform # Generates default KubernetesPlugin
```

**Result:**
```
transform/10_KubernetesPlugin/
├── resources/          # Exported resources
├── patches/            # Auto-generated cleanup patches
└── kustomization.yaml
```

### Step 3: Create Custom Stage

```bash
crane transform --stage 50_CustomLabels
```

**Result:**
```
transform/50_CustomLabels/
├── resources/          # Copied from 10_KubernetesPlugin OUTPUT
├── patches/            # Empty - ready for manual patches
└── kustomization.yaml
```

**Important:** The `resources/` in stage 50 contains the **cleaned output** from previous stage (stage 10 in this example), not the raw export!

### Step 4: Add Custom Labels and Namespace

Edit `kustomization.yaml` to add common labels and set target namespace:

```bash
cat >> transform/50_CustomLabels/kustomization.yaml <<EOF
namespace: migrated-app
commonLabels:
  migrated-with: crane
EOF
```

### Step 5: Apply All Stages

```bash
crane apply
```

Crane automatically applies **all stages sequentially**, producing final output.

## Sequential Consistency Deep Dive

**Critical concept:** Each stage sees the **fully applied output** of the previous stage.

### What This Means

```
Stage 1: Export → [Apply transforms] → Materialized Output
                                              ↓
Stage 2: Materialized Output → [Apply transforms] → Materialized Output
                                                            ↓
Stage 3: Materialized Output → [Apply transforms] → Final Output
```

### Example: Resource Deletion

**Stage 1:** Removes a Deployment (via whiteout)

`transform/10_KubernetesPlugin/`
- `resources/Deployment_apps_v1_default_myapp.yaml` exists
- Plugin marks it for whiteout (not in `kustomization.yaml` resources list)

**Stage 2:** Sees the materialized output from Stage 1

`transform/20_OpenshiftPlugin/`
- `resources/` directory **does not contain** the whitelisted Deployment
- Stage 2 never sees it
- Transformations cannot reference it

**Why it matters:**
- Stages don't see patches, they see results
- Deleted resources don't propagate
- Structural changes are visible to later stages

## Working Directory Structure

When running multi-stage transforms, Crane creates a `.work/` directory:

```
transform/
├── 10_KubernetesPlugin/
│   ├── resources/
│   ├── patches/
│   └── kustomization.yaml
├── 20_OpenshiftPlugin/
│   ├── resources/
│   ├── patches/
│   └── kustomization.yaml
└── .work/                      # Intermediate artifacts (debugging)
    ├── 10_KubernetesPlugin/
    │   ├── input/              # What stage 1 read (from export)
    │   └── output/             # What stage 1 produced (materialized)
    └── 20_OpenshiftPlugin/
        ├── input/              # What stage 2 read (stage 1 output)
        └── output/             # What stage 2 produced (materialized)
```

**Use `.work/` for debugging:**

```bash
# See what Stage 1 read
ls transform/.work/10_KubernetesPlugin/input/

# See what Stage 1 produced (input for Stage 2)
ls transform/.work/10_KubernetesPlugin/output/

# Compare input vs output
diff -r transform/.work/10_KubernetesPlugin/input/ \
        transform/.work/10_KubernetesPlugin/output/
```

**Note:** `.work/` is regenerated on each transform run. Add to `.gitignore`:

```gitignore
transform/.work/
```

## Running Specific Stages

### Run Only One Stage

```bash
# Run only stage 20 (useful for testing)
crane transform --stage 20_OpenshiftPlugin
```

**Requirement:** Previous stages must have been run and have output available.

### Run All Stages

```bash
# Default behavior: discover and run all existing stages
crane transform
```

**Note:** Plugin stages auto-regenerate; custom stages require `--force`.

### Force Re-run Everything

```bash
crane transform --force
```

**Warning:** This overwrites custom stages, including manual edits!

## Best Practices

### 1. Stage Naming Convention

Use clear, descriptive names with priority spacing:

**Good:**
```
10_KubernetesPlugin      # Core cleanup
20_OpenshiftPlugin       # Platform conversion
30_SecurityContext       # Security policies
50_CustomLabels          # Manual labels
90_FinalTweaks           # Last-minute changes
```

**Bad:**
```
1_KubernetesPlugin       # Too close together (hard to insert new stages)
2_OpenshiftPlugin
3_Custom
```

### 2. Add Manual Stages Last

**Correct:**
```
10_KubernetesPlugin     # Plugin: cleanup
20_OpenshiftPlugin      # Plugin: convert
50_CustomEdits          # Manual: tweaks (uses output from stage 20)
```

**Problem:**
```
10_KubernetesPlugin
50_CustomEdits          # Manual stage
20_OpenshiftPlugin      # Plugin added later
```

If you later re-run `20_OpenshiftPlugin`, stage 50's `resources/` directory is now stale (has old data from stage 10, not stage 20). You'd need `apply`; or `transform --force` to refresh it **losing manual edits**.

### 4. Test Incrementally

After each stage:
```bash
# Preview output
kubectl kustomize transform/<stage-name>/

# Validate syntax
kubectl apply --dry-run=client -k transform/<stage-name>/
```

## Troubleshooting Multi-Stage Pipelines

### Issue: "Stage X requires output from stage Y, but output directory does not exist"

**Cause:** You're trying to run a stage before its predecessor has been run.

**Solution:**
```bash
# Run all stages up to the one you want
crane transform

# Or run the missing predecessor
crane transform --stage 10_KubernetesPlugin
crane transform --stage 20_OpenshiftPlugin
```

### Issue: Custom stage has stale data

**Cause:** Previous plugin stages were updated, but custom stage still has old data.

**Solution:**
```bash
# Re-run with --force to refresh custom stage
crane transform --force
```

**Warning:** Manual edits in the custom stage will be lost!

### Issue: Resources missing in later stages

**Cause:** Earlier stage marked resources for whiteout (deletion).

**Solution:**
```bash
# Check what was whitelisted in earlier stage (whiteouts are commented)
cat transform/10_KubernetesPlugin/kustomize.yaml

# Inspect intermediate output
ls transform/.work/10_KubernetesPlugin/output/
```

## Summary

Multi-stage pipelines provide:
- ✅ **Separation of concerns** - Different transformations in different stages
- ✅ **Sequential consistency** - Each stage sees materialized output
- ✅ **Flexibility** - Mix plugin and manual stages
- ✅ **Debugging** - Inspect intermediate results in `.work/`
- ✅ **GitOps-friendly** - Standard Kustomize layouts

**Key Takeaways:**
- Plugin stages auto-regenerate (no `--force` needed)
- Custom stages are protected (require `--force`)
- Each stage processes previous stage's **materialized output**
- Run `crane apply` to generate manifests (between stage and final)
- Always add manual stages last
- Use `.work/` directory for debugging

## Next Steps

- [**Troubleshooting**](./05-troubleshooting.md) - Common issues and solutions
- [**Transform CLI Reference**](../transform.md) - Detailed documentation
