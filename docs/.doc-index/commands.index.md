# COMMANDS Documentation Index

## Overview
This documentation area defines the command-line interface for the Crane migration tool, covering the end-to-end lifecycle of Kubernetes workload migration. It provides instructions for exporting cluster state, performing multi-stage transformations, applying configurations, validating API compatibility, and transferring persistent volume data.

## Files Summary
*   **commands/export.md**: Details the process of discovering and writing Kubernetes resources from a source cluster to a local filesystem.
*   **commands/transform.md**: Explains how to use plugins and Kustomize stages to modify exported resources before migration.
*   **commands/apply.md**: Describes the process of rendering and applying Kustomize patches to generate final, deployable YAML manifests.
*   **commands/validate.md**: Outlines the verification process to ensure final manifests are compatible with the target cluster's API version surface.
*   **commands/transfer-pvc.md**: Details the mechanism for migrating PersistentVolumeClaim resources and underlying data between clusters using rsync pods.

## Code Changes That Would Require Documentation Updates
*   **Flag Additions/Removals**: Changing existing flags (e.g., `--crd-skip-group`) or adding new ones to any `crane` subcommand.
*   **Output Structure Changes**: Altering the directory hierarchy (e.g., changing the `export/` or `transform/` structure) or file naming conventions (e.g., changing the `Kind_group_version_namespace_name.yaml` format).
*   **Exit/Error Logic**: Modifying exit codes or error handling behavior (e.g., how the tool handles non-admin `Forbidden` errors during export).
*   **Stage Processing Logic**: Changes to how `crane transform` or `crane apply` handles priority, naming conventions (e.g., the `number_Name` requirement), or the sequential execution of stages.
*   **Plugin Architecture**: Introducing new requirements for custom plugins or changing how plugin discovery works during the transform stage.
*   **Validation Logic**: Changes to the API surface capture script or the GVK (Group-Version-Kind) matching algorithm in `crane validate`.
*   **Transfer-PVC Networking**: Adding new endpoint types (e.g., LoadBalancer) or changing how rsync pods/encryption certificates are generated in `crane transfer-pvc`.

## Key Technical Concepts
*   **Pipeline Lifecycle**: Export → Transform → Apply → Validate.
*   **GVK Matching**: Strict API Group, Version, and Kind validation.
*   **Kustomize/Krusty**: The engine used for resource patching and materialization.
*   **Multi-Stage Pipelines**: Sequential processing of transformations where the output of one stage is the input of the next.
*   **Cluster-Scoped Resources**: Handling of RBAC, CRDs, and SCCs specifically during non-admin migrations.
*   **Impersonation**: Use of `--as` and `--as-extras` for RBAC-restricted migration.
*   **Rsync Pods**: The mechanism for moving persistent data between clusters.
*   **Whiteout Resources**: Filtering resources during the transformation process.
*   **Pass-through vs. Plugin Stages**: Distinguishing between automatic plugin-based stages and manual edit stages.

## Related Components
*   **Kustomize (embedded)**: The underlying engine for resource manipulation.
*   **Kubernetes API Server**: The source and target of all resource discovery and validation.
*   **Rsync Utility**: Used by `transfer-pvc` for data synchronization.
*   **Migration Plugins**: External or internal logic providers that perform specific transformations (e.g., OpenShiftPlugin).
*   **Kubeconfig**: The configuration mechanism for authenticating against source and destination clusters.