# DEVELOPMENT Documentation Index

## Overview
This documentation area provides a comprehensive guide for developers contributing to the Crane project, covering architecture, setup, testing, and extension via the plugin system. Its purpose is to onboard new contributors and maintain consistency across the project's pipeline-based migration toolset.

## Files Summary
* **development/README.md**: Serves as the central entry point for the development documentation, linking to specialized guides and outlining the project directory structure.
* **development/setup.md**: Details the prerequisites, build instructions, project layout, IDE configurations, and instructions for setting up local test Kubernetes clusters.
* **development/plugin-development.md**: Explains the design, implementation, testing, and lifecycle of Crane plugins, including the JSONPatch-based transformation interface.
* **development/testing.md**: Outlines the testing strategy for the project, including unit testing conventions, E2E test framework usage, and CI integration requirements.
* **development/architecture.md**: Describes the core pipeline architecture of Crane, detailing the responsibilities and data flow of the `export`, `transform`, `apply`, and `validate` phases.

## Code Changes That Would Require Documentation Updates
* **Changes to CLI structure**: Adding, removing, or renaming commands in `cmd/` or altering flag registration patterns.
* **Changes to Pipeline phases**: Modifying the logic in `internal/export/`, `internal/transform/`, `internal/apply/`, or `internal/validate/`.
* **Changes to Plugin interface**: Any modifications to the stdin/stdout contract, JSONPatch requirements, or the discovery path (`~/.local/share/crane/plugins/`).
* **Changes to Project Layout**: Moving directories or packages within the `internal/` or `cmd/` structures.
* **Changes to Test framework**: Updates to the `e2e-tests/` framework or changes in the "golden manifest" comparison strategy.
* **Changes to build/CI requirements**: Updates to Go versioning in `go.mod` or modifications to GitHub Actions workflows.
* **Changes to Kustomize integration**: Updates to the `krusty` API implementation or modifications to how patches are generated/applied.

## Key Technical Concepts
* **Pipeline Architecture**: The sequential `export -> transform -> apply -> validate` workflow.
* **JSONPatch (RFC 6902)**: The format used by plugins for resource transformations.
* **Kustomize Integration**: Using the `krusty` API to manage resource patches and manifest generation.
* **Dynamic Client**: Using `dynamic.Interface` to handle Kubernetes API resources without static compilation.
* **Stage Conventions**: The `<priority>_<name>` directory structure for transformation stages.
* **Table-Driven Testing**: The standard for unit test implementation in Go.
* **Golden Manifests**: Fixtures used in E2E testing to verify transformation/export output.
* **PVC Transfer**: The specialized process for migrating Persistent Volume data using `rsync` and `stunnel`.

## Related Components
* **`cmd/`**: CLI command implementations (export, transform, apply, validate, etc.).
* **`internal/transform/`**: The Orchestrator and logic for plugin/stage management.
* **`internal/apply/`**: The embedded `krusty` Kustomize engine wrapper.
* **`internal/plugin/`**: Logic for loading and executing external plugin binaries.
* **`e2e-tests/`**: The integrated testing suite and framework.
* **`crane-lib`**: External library for core transformation logic.
* **`pvc-transfer`**: External library for handling PV migration pods.