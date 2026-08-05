# DEVELOPMENT Documentation Index

## Overview
This documentation area provides a comprehensive guide for contributors to the Crane project. It covers the end-to-end development lifecycle, including environment setup, architectural design, testing methodologies, and the extensibility of the plugin-based transformation system.

## Files Summary
- **development/setup.md**: Instructions for setting up the Go development environment, project layout, CLI development, and configuring test Kubernetes clusters.
- **development/plugin-development.md**: Guidelines for creating, naming, testing, and managing the lifecycle of transformation plugins that generate JSONPatch operations.
- **development/testing.md**: Standards for unit testing with Go, conventions for table-driven tests, and execution procedures for the E2E test framework.
- **development/architecture.md**: Technical deep-dive into the pipeline architecture (Export/Transform/Apply/Validate) and the internal data flow between disk-based stages.
- **development/README.md**: Central navigation hub and quick-reference guide for the repository structure and related projects.

## Code Changes That Would Require Documentation Updates
- **CLI Changes**: Adding, removing, or renaming CLI commands or changing how global flags (e.g., via Viper/Mapstructure) are registered.
- **Pipeline Logic**: Modifications to the sequence of the `Export -> Transform -> Apply -> Validate` data flow or changes to how data is persisted to the local directory structure.
- **Plugin System**: Changes to the plugin interface (stdin/stdout contract), the discovery path (default directory), or the priority/stage execution logic.
- **Internal API/Library Usage**: Upgrading or swapping the embedded Kustomize (`krusty`) library or changing the `unstructured` resource handling patterns.
- **Testing Framework**: Additions or breaking changes to the `e2e-tests/framework` package or the format of `golden-manifests`.
- **Project Structure**: Renaming or moving packages within `cmd/` or `internal/` or modifying the project layout patterns.

## Key Technical Concepts
- **JSONPatch (RFC 6902)**: The mechanism used by plugins to define resource transformations.
- **Kustomize (krusty API)**: The engine embedded within Crane to apply patches and render manifests.
- **Stage Discovery**: The `<number>_<name>` directory convention used to determine execution order.
- **Unstructured API**: The `k8s.io/apimachinery` pattern used for processing dynamic Kubernetes resources.
- **Dynamic Client**: The interface used by the export phase to handle arbitrary resource types.
- **Table-Driven Testing**: The standard pattern required for Go unit tests.
- **Golden Manifests**: The fixture-based comparison method used for E2E validation.
- **PVC Transfer**: The specific sub-process using `pvc-transfer` for data migration.

## Related Components
- **`cmd/`**: CLI entry points and command constructors.
- **`internal/`**: The core logic (orchestrator, kustomize runner, plugin loader, validator).
- **`e2e-tests/`**: The integration testing framework and suite.
- **`crane-lib`**: External library containing common transformation logic (e.g., `KubernetesPlugin`).
- **`crane-plugins` / `crane-plugin-openshift`**: External repositories for platform-specific transformations.
- **`pvc-transfer`**: The underlying library for persistent volume data migration.