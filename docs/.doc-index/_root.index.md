# _ROOT Documentation Index

## Overview
This documentation covers the operational workflow, architecture, and configuration of **Crane**, a Kubernetes migration tool. It describes the end-to-end process of exporting, transforming, validating, and applying cluster resources, including the plugin-based multi-stage pipeline and compatibility requirements.

## Files Summary
* **kustomize-multistage.md**: Describes the sequential, plugin-driven transformation pipeline using Kustomize and directory-based staging.
* **CRANE_COMPATIBILITY_MATRIX.md**: Outlines the support boundaries between namespace-scoped and cluster-scoped resources during migration.
* **pre-apply-validation-guide.md**: Provides a checklist and scripts for validating manifests against cluster APIs and permissions before deployment.
* **plugins.md**: Details how to use, write, and manage plugins for resource transformation.
* **transform.md**: Explains the directory structure, Kustomize configuration, and manual editing workflows for the transformation pipeline.
* **resource-compatibility.md**: A detailed reference on resource migration boundaries, RBAC requirements, and namespace-renaming warnings.
* **multistage-pipeline.md**: A comprehensive guide to the multi-stage pipeline, including CLI usage, priority assignment, and stage chaining.
* **stateless-migration-quickstart.md**: A step-by-step tutorial for performing a basic stateless migration from export to apply.
* **installation.md**: Instructions for building, installing, and verifying the Crane CLI.
* **README.md**: The top-level entry point and documentation hub for the project.

## Code Changes That Would Require Documentation Updates
* **Pipeline Logic**: Changes to the transformation sequence, stage directory discovery, or the `kustomization.yaml` generation logic.
* **Plugin Interface**: Changes to how plugins interact with stdin/stdout, input/output structures, or how they are discovered in `~/.local/share/crane/plugins/`.
* **CLI Commands**: Addition, removal, or parameter changes for `crane export`, `crane transform`, `crane apply`, or `crane validate`.
* **Resource Support**: Updates to the `crane-lib` logic that handles resource extraction or server-managed field removal (e.g., adding new exclusions).
* **Validation Logic**: Changes to the `kubectl` dry-run mechanisms or `kubectl auth can-i` checks performed by the validation command.
* **Configuration/Metadata**: Changes to `.crane-metadata.json` or any requirements for the `instructions.yaml` file.
* **Default Behaviors**: Alterations to default plugin priorities or automatic stage creation logic.

## Key Technical Concepts
* **Multi-Stage Pipeline**: The sequential execution of transform stages based on numeric priority.
* **Kustomize Integration**: Use of `kustomization.yaml` and patches to mutate manifests.
* **Dirty Check**: Protection mechanism preventing the overwrite of manually edited stages without the `--overwrite` flag.
* **Whiteout**: The process of excluding resources from the final output.
* **Stateless Migration**: The `export` → `transform` → `apply` → `validate` workflow.
* **Plugin Priority**: Numeric assignment (e.g., `10_`, `20_`) determining the order of transformation.
* **Server-Managed Fields**: Metadata like `uid` and `resourceVersion` that require removal during cross-cluster migration.
* **Live Validation**: Using `kubectl --dry-run=server` to check manifest compatibility against the target cluster.

## Related Components
* **Crane-lib**: The underlying library used for plugin definitions and resource cleanup logic.
* **Kustomize**: The engine used for patching and resource materialization within stages.
* **Crane-plugins**: The repository of community-contributed plugins (e.g., OpenshiftPlugin).
* **Kubectl/OC**: External CLI tools required for live cluster interaction and validation.
* **Go Runtime**: The base environment for the Crane CLI and custom plugin development.