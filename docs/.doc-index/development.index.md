# DEVELOPMENT Documentation Index

## Overview
This documentation area provides a comprehensive guide for developers contributing to the Crane project. It covers project architecture, local development environment setup, plugin creation, testing procedures, and the end-to-end data pipeline lifecycle.

## Files Summary
* **`development/README.md`**: Serves as the central landing page and navigational index for all developer-focused documentation.
* **`development/setup.md`**: Details environment prerequisites, repository cloning, building from source, IDE configurations, and instructions for setting up test clusters.
* **`development/plugin-development.md`**: Explains the plugin system architecture, interface specifications (stdin/stdout), creation of Go/Bash plugins, and stage-based priority execution.
* **`development/testing.md`**: Outlines standards for unit testing, table-driven test patterns, E2E testing using `kind`/`minikube`, and CI/CD integration requirements.
* **`development/architecture.md`**: Describes the core "pipeline" philosophy, covering the design and flow of the Export, Transform, Apply, Validate, and PVC transfer phases.

## Code Changes That Would Require Documentation Updates
* **Command Structure Changes**: Adding, removing, or modifying CLI commands (requires updating `cmd/` directory documentation in `setup.md`).
* **Pipeline Logic Updates**: Changes to how stages are discovered, how plugins are executed, or updates to the `orchestrator` logic in `internal/transform/`.
* **Plugin Interface Changes**: Altering the stdin/stdout contract for plugins or the naming convention for stage directories.
* **Kustomize Integration**: Modifying how `krusty` (embedded kustomize) is invoked or how patch files are generated/applied.
* **API/Discovery Logic**: Updates to how `dynamic.Interface` or Kubernetes discovery clients are used in the export or validation phases.
* **New Supported Platforms**: Adding platform-specific transformations or changing how cluster-scoped resources are handled.
* **Testing Infrastructure**: Changes to the `e2e-tests/` framework or the introduction of new mock/helper utilities.
* **Dependency Changes**: Updates to the Go version, external libraries, or build dependencies listed in `setup.md`.

## Key Technical Concepts
* **Pipeline Stages**: `export`, `transform`, `apply`, `validate`, `transfer-pvc`.
* **Plugin System**: JSONPatch (RFC 6902), `~/.local/share/crane/plugins/`, stage priority numeric prefixes.
* **Infrastructure**: `kustomize` (krusty API), Kubernetes `unstructured.Unstructured`, `cobra` (CLI framework), `viper` (flags).
* **Test Patterns**: Table-driven tests, `t.TempDir()`, `golden-manifests`, E2E test framework.
* **Orchestration**: `Orchestrator`, `Stage` discovery, `KustomizeApplier`.

## Related Components
* **`cmd/`**: CLI entry points for all functional phases.
* **`internal/`**: Core business logic packages including `transform/`, `apply/`, `validate/`, and `plugin/`.
* **`e2e-tests/`**: The dedicated suite for integration and regression testing.
* **`crane-lib`**: External library containing common transformation utilities.
* **`pvc-transfer`**: External subsystem used for data volume migrations.