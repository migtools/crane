# _ROOT Documentation Index

## Overview
This documentation area provides a comprehensive guide to the **Crane** Kubernetes migration tool, covering the entire end-to-end migration pipeline: `export`, `transform`, `apply`, and `validate`. It serves as the primary reference for operators and developers regarding pipeline architecture, plugin development, deployment, and cluster-to-cluster workload migration workflows.

## Files Summary
*   **README.md**: Serves as the central navigation hub for the Crane project, detailing command references, concepts, development guides, and contributing policies.
*   **installation.md**: Outlines prerequisites, installation methods (binary vs. source), verification steps, and basic cluster connectivity testing.
*   **multistage-pipeline.md**: Explains the core architectural mechanism of Crane, describing how stages, plugins, and Kustomize work together to clean and adapt manifests.
*   **plugins.md**: Documents the plugin system, listing built-in and community plugins, management commands, and requirements for writing custom transformation logic.
*   **pre-apply-validation-guide.md**: Provides a safety-critical checklist for verifying manifests against target clusters, including dry-run commands, RBAC checks, and dependency verification.
*   **resource-compatibility.md**: Defines the operational scope and migration boundaries, detailing how Crane handles namespace-scoped vs. cluster-scoped resources and mapping requirements.
*   **stateless-migration-quickstart.md**: Offers a step-by-step practical tutorial for executing a full stateless migration workflow from source to target clusters.

## Code Changes That Would Require Documentation Updates
*   **CLI Command Signatures**: Adding, removing, or renaming flags (e.g., `--export-dir`, `--skip-plugins`) or changing positional arguments for commands.
*   **Plugin API/Lifecycle**: Changes to how plugins are discovered, executed, or their expected input/output format (JSONPatch) on `stdin/stdout`.
*   **Default Pipeline Behavior**: Modifying the default priority order, the auto-naming of stages, or the default inclusion of the `KubernetesPlugin`.
*   **Core Architecture**: Changes to the file-per-resource vs. multi-document grouping strategy or the integration of Kustomize.
*   **Dirty-Check Logic**: Changes to how Crane detects user modifications in transform stages or the behavior of the `--force` flag.
*   **Validation Logic**: Updates to `crane validate` logic, such as adding new compatibility checks or changing the format of the `report.json`.
*   **Supported Resource Types**: Changes to how Crane discovers or handles specific API groups, CRDs, or cluster-scoped resources.
*   **Embedded Dependencies**: Upgrading or removing the embedded Kustomize version or Go dependencies that affect build prerequisites.

## Key Technical Concepts
*   **Migration Pipeline**: The sequence of `export` → `transform` → `apply` → `validate`.
*   **Multi-Stage Pipeline**: Sequential transformation stages organized by priority (`<priority>_<plugin-name>`).
*   **Kustomize/JSONPatch**: The underlying technologies used to apply modifications to Kubernetes manifests.
*   **Stage Chaining**: The passing of output from one transformation stage as the input for the next.
*   **Dirty-Check**: Protection mechanism that prevents overwriting manual user edits in transform directories.
*   **Live/Server-Side Dry-Run**: Validation mode using the target API server to check schema, policy, and quota compliance.
*   **Cluster-Scoped Resources**: Resources like ClusterRoles, CRDs, or SCCs that require specific permission contexts.
*   **Plugin Directory**: The location (`~/.local/share/crane/plugins/`) where executable binaries are stored for runtime discovery.

## Related Components
*   **Crane CLI (`crane`)**: The main binary and entry point for all operations.
*   **Crane Lib (`crane-lib`)**: The underlying library containing core logic and the built-in `KubernetesPlugin`.
*   **Kustomize**: The embedded rendering engine used for generating final manifests.
*   **Plugin Subsystem**: The interface and discovery mechanism for external transformation binaries.
*   **Validation Engine**: The module responsible for `kubectl` dry-run interactions and compatibility reporting.
*   **Staging Engine**: The file-system management layer that organizes inputs, patches, and outputs into directory structures.