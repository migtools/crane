# COMMANDS Documentation Index

## Overview
This documentation area provides comprehensive instructions for the `crane` CLI toolset, a migration pipeline designed for moving Kubernetes resources between clusters. It covers the full lifecycle of a migration, including resource exportation, transformation via Kustomize plugins, cluster-to-cluster data transfer, manifest application, and validation against target cluster API surfaces.

## Files Summary
- **commands/apply.md**: Describes how to run embedded Kustomize on transform stages to generate final, deployable YAML manifests.
- **commands/export.md**: Details the discovery and collection of Kubernetes API objects and CRDs from a source cluster into an export directory.
- **commands/transfer-pvc.md**: Explains procedures for migrating PersistentVolumeClaim resources and their underlying volume data using either direct network connectivity or indirect cloud storage.
- **commands/transform.md**: Documents the multi-stage transformation pipeline, including how to use plugins, custom manual stages, and sequential Kustomize patch application.
- **commands/validate.md**: Outlines the process for verifying final rendered manifests against a target cluster’s API surface, supporting both live and offline validation modes.

## Code Changes That Would Require Documentation Updates
*   **CLI Flags/Arguments**: Addition, removal, or renaming of any flag, short code, or default value (e.g., adding a new `--log-level` or modifying `--ordered`).
*   **Pipeline Logic**: Changes to the order of operations, sequential consistency rules, or how `crane` creates/manages directories (`export/`, `transform/`, `output/`, `validate/`).
*   **Plugin Architecture**: Modifications to how plugins are registered, the naming convention (`*Plugin`), or the automatic stage creation logic.
*   **Transfer Methods**: Introduction of new volume transfer protocols, changes to the `rsync` Pod implementation, or updates to supported cloud storage providers (S3/MinIO).
*   **Default Behavior**: Any change to default output formats (e.g., changes to the file naming convention `Kind_group_version...`), file structures, or default directory locations.
*   **API Interactions**: Changes to impersonation logic, RBAC requirements for non-admin migrations, or how the tool handles `Forbidden` errors.
*   **Validation Logic**: Updates to GVK matching logic, the format of the API surface JSON, or the introduction of new validation rules.

## Key Technical Concepts
*   **GVK (Group, Version, Kind)**: The fundamental schema used for validation and resource identification.
*   **Kustomize / Krusty API**: The embedded engine used for transforming manifests during the `apply` and `transform` phases.
*   **Sequential Consistency**: The pipeline rule where each transformation stage operates on the materialized output of the previous stage.
*   **Whiteout**: The concept of filtering or deleting resources during the transformation pipeline.
*   **Direct vs. Indirect Transfer**: Migration modes for PVCs (Direct via `rsync`/Ingress/Route vs. Indirect via S3-compatible storage/rclone).
*   **Impersonation**: Using `--as` and `--as-extras` flags to perform migrations with specific service account permissions.
*   **Cluster-Scoped Resources**: Resources like `ClusterRole`, `ClusterRoleBinding`, and `CRDs` that are handled specifically during migration.
*   **Pass-through vs. Plugin Stages**: The distinction between automated plugin-driven stages and manually editable custom stages.

## Related Components
*   **Crane Core**: The primary CLI entry point and orchestration layer.
*   **Kubernetes API**: The source and target environment for all resource operations.
*   **Kustomize Engine**: The embedded resource manipulation library.
*   **Rsync Daemon/Pod**: The data synchronization utility used in `transfer-pvc`.
*   **Rclone**: The backend tool utilized for indirect cloud storage transfers.
*   **Kubeconfig**: The authentication and cluster-context management system.