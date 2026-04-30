# Crane Transform - Scenarios and Tutorials

Welcome to the Crane Transform documentation! This directory contains comprehensive tutorials and scenario-based guides to help you master `crane transform`.

## What is Crane Transform?

Crane Transform is the second phase in Crane's Kubernetes migration workflow. It prepares exported resources for deployment to a target cluster by:

- **Cleaning resources** - Removing cluster-specific metadata
- **Applying transformations** - Modifying resources for target environments
- **Organizing artifacts** - Creating Kustomize-based directory structures

## Quick Navigation

### For Beginners

Start here if you're new to crane transform:

1. **[Overview](./01-overview.md)** - Understand what crane does and key concepts
2. **[Quickstart Tutorial](./02-quickstart.md)** - Hands-on walkthrough with a real example

### For Intermediate Users

Explore advanced features:

3. **[Multi-Stage Pipelines](./03-multistage.md)** - Chain multiple transformation stages

### For Troubleshooting

4. **[Troubleshooting & FAQ](./05-troubleshooting.md)** - Common issues, error messages, and solutions

## What's Covered

### 01 - Overview
- **What you'll learn:**
  - What crane transform does
  - Why it's needed
  - Key concepts (plugins, stages, Kustomize)
  - Common use cases
  - Directory structure

- **Best for:** Understanding the big picture

### 02 - Quickstart Tutorial
- **What you'll learn:**
  - Complete workflow from export to apply
  - Deploy sample app
  - Run your first transform
  - Inspect generated artifacts
  - Deploy to target cluster

- **Best for:** Hands-on learning
- **Prerequisites:** kubectl, access to a cluster

### 03 - Multi-Stage Pipelines
- **What you'll learn:**
  - When to use multiple stages
  - Plugin vs custom stages
  - Sequential consistency
  - Working directory structure
  - Manual editing with Kustomize features
  - Best practices

- **Best for:** Complex transformations and manual customization
- **Prerequisites:** Completed quickstart

### 05 - Troubleshooting & FAQ
- **What you'll learn:**
  - Common error messages
  - Debugging techniques
  - Solutions to typical problems
  - Frequently asked questions

- **Best for:** Solving problems

## Common Scenarios

### Scenario 1: Simple Cluster-to-Cluster Migration
**Goal:** Migrate app from one Kubernetes cluster to another (same type)

**Steps:**
1. [Quickstart Tutorial](./02-quickstart.md) - Follow the complete walkthrough
2. Deploy using `kubectl apply -f output/output.yaml`

---

### Scenario 2: OpenShift to Kubernetes Migration
**Goal:** Migrate app from OpenShift to vanilla Kubernetes

**Steps:**
1. [Multi-Stage Pipelines](./03-multistage.md) - See OpenShift migration example
2. Use `10_KubernetesPlugin` for cleanup
3. Use `20_OpenshiftPlugin` for conversion
4. Add custom stage for environment tweaks

---

### Scenario 3: GitOps Workflow
**Goal:** Prepare resources for ArgoCD/Flux

**Steps:**
1. [Quickstart Tutorial](./02-quickstart.md) - Generate transformed resources
2. Commit `transform/` directory to Git
3. Configure ArgoCD/Flux to deploy from Git

**Reference:** [FAQ - GitOps integration](./05-troubleshooting.md#q-can-i-use-transform-output-with-argocdflux)

---

### Scenario 4: Multi-Environment Deployment
**Goal:** Prepare resources for dev, staging, production

**Steps:**
1. [Multi-Stage Pipelines](./03-multistage.md) - See environment-specific overlays
2. Create base transformation
3. Use Kustomize overlays for environments

---

### Scenario 5: Manual Customization
**Goal:** Hand-edit specific resources

**Steps:**
1. [Multi-Stage Pipelines](./03-multistage.md) - See custom stages section
2. Create custom stage
3. Add manual edits or patches with Kustomize features

---

## Learning Path

### Path 1: Quick Start
For users who want to get started quickly:

1. Read [Overview](./01-overview.md)
2. Complete [Quickstart](./02-quickstart.md)
3. Browse [Troubleshooting](./05-troubleshooting.md)

**Outcome:** Can perform basic transforms and migrations

---

### Path 2: Comprehensive
For users who want deep understanding:

1. Read [Overview](./01-overview.md)
2. Complete [Quickstart](./02-quickstart.md)
3. Study [Multi-Stage Pipelines](./03-multistage.md)
4. Review [Troubleshooting](./05-troubleshooting.md)

**Outcome:** Can handle complex migrations with confidence

---

### Path 3: Problem-Solving
For users encountering issues:

1. Go directly to [Troubleshooting](./05-troubleshooting.md)
2. Search for your error message or symptom
3. Follow the solution steps

**Outcome:** Resolve specific issues quickly

---

## Additional Resources

### Official Documentation

- **[Transform CLI Reference](../transform.md)** - Detailed directory structure and options
- **[Multi-Stage Kustomize](../kustomize-multistage.md)** - Technical deep-dive into architecture
- **[Crane README](../../README.md)** - Project overview and installation

### External Resources

- **[Kustomize Documentation](https://kubectl.docs.kubernetes.io/references/kustomize/)** - Learn Kustomize features
- **[Konveyor Community](https://www.konveyor.io/)** - Crane parent project
- **[JSONPatch Specification](https://jsonpatch.com/)** - Understand patch syntax

### Example Data

This directory includes example exported resources in `export/resources/` for reference:
- WordPress + MySQL deployment
- Demonstrates typical exported resources
- Shows what transform processes

## Quick Reference

### Common Commands

```bash
# Export from source cluster
crane export

# Transform with default stage
crane transform

# Create custom stage
crane transform --stage 50_CustomEdits

# Force overwrite (WARNING: loses manual edits)
crane transform --force

# List available plugins
crane transform list-plugins

# Preview final output
kubectl kustomize transform/10_KubernetesPlugin/

# Generate final manifests
crane apply

# Apply to target cluster
kubectl apply -f output/output.yaml
```

### Directory Structure

```
.
├── export/                      # Phase 1: Exported resources
│   └── resources/
│
├── transform/                   # Phase 2: Transform stages
│   ├── 10_KubernetesPlugin/    # Plugin-based stage
│   │   ├── resources/
│   │   ├── patches/
│   │   └── kustomization.yaml
│   ├── 50_CustomEdits/         # Custom stage
│   │   └── ...
│   └── .work/                  # Intermediate artifacts (debugging)
│
└── output/                      # Phase 3: Final manifests
    ├── output.yaml
    └── resources/
```

### Plugin vs Custom Stages

| Feature | Plugin Stage | Custom Stage |
|---------|-------------|--------------|
| **Name pattern** | Ends with `Plugin` | Does NOT end with `Plugin` |
| **Example** | `10_KubernetesPlugin` | `50_CustomEdits` |
| **Behavior** | Runs plugin | Pass-through (no plugin) |
| **Regeneration** | Always (auto) | Protected (requires `--force`) |
| **Use case** | Automated cleanup | Manual customization |

### Workflow Phases

```
1. crane export        →  export/resources/
2. crane transform     →  transform/stages/
3. crane apply         →  output/output.yaml
4. kubectl apply       →  target cluster
```

## Getting Help

### Common Issues

**Issue:** Error messages
- **Solution:** Check [Troubleshooting - Common Errors](./05-troubleshooting.md#common-errors)

**Issue:** Resources missing from output
- **Solution:** Check [Troubleshooting - Resource Issues](./05-troubleshooting.md#resource-issues)

**Issue:** Kustomize syntax errors
- **Solution:** Check [Troubleshooting - Kustomize Issues](./05-troubleshooting.md#kustomize-issues)

### Support Channels

- **GitHub Issues:** [konveyor/crane/issues](https://github.com/konveyor/crane/issues)
- **Documentation:** This directory and [docs/](../)
- **Community:** Konveyor Slack

## Contributing

Found an issue or want to improve these docs?

1. **Report issues:** [File a bug](https://github.com/konveyor/crane/issues)
2. **Suggest improvements:** Open a PR
3. **Share scenarios:** Contribute new examples

## What's Next?

**New to crane transform?**
- Start with [Overview](./01-overview.md)

**Want hands-on practice?**
- Jump to [Quickstart Tutorial](./02-quickstart.md)

**Need advanced features?**
- Explore [Multi-Stage Pipelines](./03-multistage.md)

**Encountering issues?**
- Check [Troubleshooting](./05-troubleshooting.md)
