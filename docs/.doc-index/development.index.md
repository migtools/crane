# DEVELOPMENT Documentation Index

## Overview
This documentation area provides a comprehensive guide for contributors to the Crane project. It covers the technical architecture, development environment configuration, testing standards, and the plugin-based transformation system required to maintain and extend the tool.

## Files Summary
*   **development/README.md**: Serves as the primary entry point and navigator for all developer-focused documentation and project structure.
*   **development/setup.md**: Details the prerequisites, environment setup, build procedures, and coding conventions for working on the Crane repository.
*   **development/plugin-development.md**: Explains the architecture, interface, naming conventions, and best practices for creating custom transformation plugins.
*   **development/testing.md**: Describes unit and E2E testing strategies, including table-driven test patterns, test infrastructure, and CI requirements.
*   **development/architecture.md**: Provides a deep dive into the pipeline-based data flow, covering the export, transform, apply, validate, and PVC transfer phases.

## Code Changes That Would Require Documentation Updates
*   **CLI Command Structure**: Adding, renaming, or changing the signature of CLI commands under `cmd/`.
*   **Pipeline Stages**: Any modification to how stages are discovered, executed, or persisted in the `transform/` directory.
*   **Plugin Interface**: Changes to the stdin/stdout contract, JSONPatch structure, or plugin discovery paths (e.g., changing the default `~/.local/share/crane/plugins/` directory).
*   **Kustomize Integration**: Changes to the embedded `krusty` API usage or how `kustomization.yaml` files are generated.
*   **API Interactions**: Modifications to how `unstructured.Unstructured` objects are handled or how CRDs are collected during export.
*   **Validation Logic**: Updates to how compatibility reports are generated or how the `validate` phase queries cluster API surfaces.
*   **Testing Infrastructure**: Changes to the E2E framework under `e2e-tests/` or the introduction of new mock/helper utilities.
*   **Project Layout**: Moving files out of `internal/` or changing the directory hierarchy.

## Key Technical Concepts
*   **Pipeline Architecture**: Export → Transform → Apply → Validate flow.
*   **JSONPatch (RFC 6902)**: Used for resource transformations via plugins.
*   **Kustomize/Krusty**: Embedded Kubernetes configuration management library.
*   **Plugin Stages**: Naming convention `<priority>_<PluginName>`.
*   **Unstructured API**: `k8s.io/apimachinery/pkg/apis/meta/v1/unstructured`.
*   **Table-Driven Tests**: The standard pattern for Go testing in the project.
*   **Discovery Client**: Used for dynamic Kubernetes resource listing.
*   **Stage Orchestrator**: The logic in `internal/transform/` managing sequential consistency.
*   **Golden Manifests**: Fixtures used in `e2e-tests/` for verification.

## Related Components
*   **`cmd/`**: CLI command entry points.
*   **`internal/`**: Core business logic packages (apply, transform, validate, plugin, kustomize).
*   **`e2e-tests/`**: Integration and regression testing framework.
*   **`crane-lib`**: External library for built-in transformation logic.
*   **`pvc-transfer`**: External library used for persistent volume migration.
*   **Kubernetes API/Discovery**: The source of truth for cluster-scoped and namespaced resources.