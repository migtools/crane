# DEVELOPMENT Documentation Index

## Overview
This documentation area provides a comprehensive guide for developers contributing to the Crane project. It covers the system's pipeline-based architecture, local development environment setup, the plugin development lifecycle, and testing standards required for maintaining project health.

## Files Summary
*   **development/README.md**: Serves as the primary entry point for developers, outlining the repository structure, quick-reference commands, and links to specialized guides.
*   **development/setup.md**: Details the prerequisites, project layout, building instructions, local installation methods, and IDE configurations.
*   **development/plugin-development.md**: Explains the plugin system interface, stage naming conventions, priority execution, and instructions for creating plugins in Go or Bash.
*   **development/testing.md**: Describes unit testing conventions, table-driven test patterns, E2E test architecture, and CI integration requirements.
*   **development/architecture.md**: Provides a deep dive into the internal data flow, the multi-stage pipeline, and the implementation details of the export, transform, apply, and validate phases.

## Code Changes That Would Require Documentation Updates
*   **CLI Command Changes**: Adding, removing, or renaming commands in `cmd/`, or modifying the `Options` struct/flag registration logic.
*   **Pipeline Logic Changes**: Modifications to the orchestrator, stage discovery patterns, or the sequential execution logic in `internal/transform/`.
*   **Plugin System API**: Changes to the stdin/stdout contract, the discovery path (`~/.local/share/crane/plugins/`), or the stage naming convention (`<priority>_<Name>`).
*   **Dependencies**: Updating the required Go version, modifying the embedded Kustomize usage (`krusty` API), or changing required tools like `kubectl` or cluster providers.
*   **Testing Infrastructure**: Changes to the `e2e-tests/` framework, the introduction of new test fixtures (golden manifests), or modifications to CI/CD workflows in `.github/workflows/`.
*   **Architecture Flow**: Introducing new phases to the pipeline or changing the underlying data storage path structure (e.g., changing how `export/`, `transform/`, or `output/` directories are managed).

## Key Technical Concepts
*   **Pipeline Stages**: Export, Transform, Apply, Validate.
*   **Plugin System**: JSONPatch (RFC 6902), stdin/stdout plugin interface, stage priority indexing.
*   **Kustomize Integration**: `krusty` API, `kustomization.yaml` generation.
*   **Cluster Management**: `kind`, `minikube`, E2E test clusters.
*   **Kubernetes API Handling**: `unstructured.Unstructured`, dynamic client, CRD collection, server-managed field removal.
*   **Testing**: Table-driven tests, `t.TempDir()`, `golden-manifests`, `go test -race`.
*   **Project Structure**: `cmd/` (wrappers/constructors), `internal/` (business logic).

## Related Components
*   **crane-lib**: Core library containing common transformations and the `KubernetesPlugin`.
*   **pvc-transfer**: Underlying library for migrating persistent volumes.
*   **Kustomize**: The engine embedded via `krusty` for manifest modification.
*   **Go Modules**: Project-wide dependency management via `go.mod`.
*   **CI/CD Workflows**: GitHub Actions configurations for unit and E2E validation.