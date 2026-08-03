# _ROOT Documentation Index

## Overview
This documentation provides a comprehensive guide to the **Crane** migration tool, focusing on its multi-stage Kustomize-based transformation pipeline, resource migration compatibility, and manifest validation workflows. It covers the full lifecycle of a migration from cluster export and transformation through to final deployment and target cluster compatibility checks.

## Files Summary
* **kustomize-multistage.md**: Describes the architecture and usage of the multi-stage Kustomize pipeline, including directory structures, plugin priorities, and stage chaining.
* **CRANE_COMPATIBILITY_MATRIX.md**: Defines the operational boundaries of Crane regarding namespace-scoped vs. cluster-scoped resources and outlines the prerequisites for successful migrations.
* **pre-apply-validation-guide.md**: Provides a checklist and scripts for validating Kubernetes manifests against a target cluster API and RBAC permissions before execution.
* **transform.md**: Details the internal structure of the `transform/` directory, including how to work with input/output, manual edits, and Git best practices.
* **stateless-migration-quickstart.md**: Offers a step-by-step tutorial for executing a complete stateless migration workflow, from export to final application on a target cluster.

## Code Changes That Would Require Documentation Updates
* **Pipeline Logic**: Changes to the stage discovery, ordering algorithm, or the `crane transform` execution flow.
* **Plugin System**: Changes to how plugins are loaded, how priorities are assigned, or changes to the built-in `KubernetesPlugin`.
* **File Structure**: Changes to the naming conventions of `transform/` subdirectories (`input/`, `patches/`, `output/`) or the `kustomization.yaml` requirements.
* **CLI Arguments/Flags**: Addition, removal, or modification of flags for `crane transform`, `crane apply`, `crane export`, or `crane validate`.
* **Output Formats**: Changes to the structure of the final `output/` directory or the `output.yaml` manifest generation.
* **Validation Logic**: Updates to how `crane validate` performs live-cluster compatibility checks or how it generates reports.
* **Whiteout/Filtering**: Changes to how resources are excluded (whiteout) or filtered during the transformation phase.

## Key Technical Concepts
* **Multi-Stage Pipeline**: Sequential transformation process where stage output becomes next-stage input.
* **Kustomize**: The underlying engine used for resource patching and manifest management.
* **Stage Directory Structure**: The `<priority>_<plugin-name>` convention used to organize transformations.
* **Plugin Priorities**: Numeric ordering (e.g., 10, 20, 30) for determining the execution order of transform plugins.
* **Dirty Check**: A safety mechanism that prevents overwriting manual user edits to transform stages.
* **Pass-through Stage**: A manual transformation stage that lacks a plugin and persists user-applied changes.
* **Server-side Dry-run**: `kubectl apply --dry-run=server` validation against the target API server.
* **RBAC Context**: The permissions required to export/import specific resource types (e.g., SCCs, CRDs).
* **Whiteout**: The process of excluding resources from the final migration output.

## Related Components
* **Crane CLI**: The primary user-facing tool for migration orchestration.
* **crane-lib**: The library containing built-in plugins (e.g., `kubernetes` plugin).
* **Kustomize**: Integrated directly into the `transform` and `apply` phases.
* **Kubernetes API Server**: Target for validation and deployment.
* **Git**: Used for version controlling the `transform/` directory configuration.