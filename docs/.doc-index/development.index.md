# DEVELOPMENT Documentation Index

## Overview
This documentation area provides a comprehensive guide for developers contributing to the Crane project. It covers the architecture, development environment setup, the plugin system, testing strategies, and project structure, serving as the primary resource for extending and maintaining the codebase.

## Files Summary
- **development/README.md**: Provides a high-level project overview, directory structure, and quick reference for build and test commands.
- **development/architecture.md**: Details the pipeline architecture (export, transform, apply, validate), data flow, and key components like the Orchestrator and Writer.
- **development/plugin-development.md**: Explains how to create, test, and package custom transformation plugins, including the required plugin interface and lifecycle.
- **development/setup.md**: Outlines prerequisites, installation procedures, project layout, and standard IDE configurations for developers.
- **development/testing.md**: Describes unit and E2E testing strategies, including table-driven test patterns, golden manifests, and CI integration.

## Code Changes That Would Require Documentation Updates
- **Pipeline Logic**: Any changes to the `export/transform/apply/validate` sequence or the way data is passed between stages.
- **CLI Structure**: Adding, removing, or renaming commands in `cmd/`, or changes to global flag management.
- **Internal API/Interfaces**: Modifications to the `Orchestrator`, `Writer`, or `Stage` interfaces in `internal/transform/`.
- **Plugin Protocol**: Changes to the input/output JSON formats (stdin/stdout) for plugins or the plugin discovery path.
- **Repository Structure**: Any significant reorganization of the `internal/` or `cmd/` directory trees.
- **Dependencies**: Updates to external libraries that change how Kustomize is embedded or how Kubernetes objects are manipulated (e.g., changing from `unstructured` objects).
- **Testing Framework**: Additions or removals of test helper utilities or changes to how `golden-manifests` are validated in the E2E suite.

## Key Technical Concepts
- **Pipeline Stages**: The sequential execution of Export, Transform, Apply, and Validate.
- **JSONPatch (RFC 6902)**: The primary mechanism for resource transformation.
- **Kustomize Integration**: Using `krusty` for server-side manifest rendering without external CLI dependencies.
- **Kubernetes Dynamic Client**: Used for listing arbitrary resources without compile-time schema knowledge.
- **Plugin Lifecycle**: Discovery, stdin/stdout interaction, and naming conventions (`priority_NamePlugin`).
- **Stage Ordering**: Numeric prefixes (`10_`, `20_`, etc.) to control execution order.
- **Orchestrator**: The logic coordinating multi-stage transformation execution.
- **Golden Manifests**: Fixture-based E2E verification.

## Related Components
- **`cmd/`**: CLI entry points and command implementation.
- **`internal/transform/`**: The transformation engine and stage management.
- **`internal/apply/`**: Embedded Kustomize logic.
- **`internal/validate/`**: Compatibility scanning and reporting.
- **`e2e-tests/`**: End-to-end integration test suite.
- **`konveyor/crane-lib`**: External transformation helper library.
- **`pvc-transfer`**: Library for handling storage volume migration.