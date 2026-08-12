# COMMANDS Documentation Index

## Overview
This documentation area defines the CLI tool `crane`, which orchestrates Kubernetes cluster-to-cluster migrations. It covers the full lifecycle of a migration project, including resource extraction, transformation pipelines, validation against target clusters, and persistent volume data migration.

## Files Summary
*   **commands/export.md**: Details the discovery and extraction process of Kubernetes resources (including CRDs and cluster-scoped items) from a source cluster into local YAML files.
*   **commands/transform.md**: Explains the multi-stage transformation pipeline using Kustomize and plugins to modify, patch, or filter resources before they are applied.
*   **commands/validate.md**: Describes how to verify the compatibility of transformed manifests against a target cluster’s API surface, supporting both live and offline modes.
*   **commands/apply.md**: Covers the process of rendering final manifests by running embedded Kustomize on transform stages and preparing them for cluster deployment.
*   **commands/transfer-pvc.md**: Details the mechanism for transferring PersistentVolumeClaim resources and underlying data between clusters using rsync and secure, self-signed connections.

## Code Changes That Would Require Documentation Updates
*   **CLI Flags**: Addition, removal, or renaming of any flag (e.g., adding `--dry-run` or changing default QPS settings).
*   **Output Structure**: Changes to the file organization of `export/`, `transform/`, or `output/` directories.
*   **Pipeline Logic**: Modifications to the sequential nature of `crane apply` or the interaction between transform stages (e.g., how input/output directories are handled).
*   **API/GVK Handling**: Changes to how `crane validate` identifies or verifies API versions and kinds, or how `crane export` discovers cluster-scoped resources.
*   **Plugin Architecture**: Changes to how plugins are loaded, registered, or how their priorities/naming conventions function.
*   **Transfer Logic**: Updates to the `transfer-pvc` rsync process, including new endpoint types, encryption methods, or pod templates used for data movement.
*   **Default Behaviors**: Changes to exit codes, error handling, or default settings (e.g., changing the default behavior for cluster-scoped resource inclusion).

## Key Technical Concepts
*   **GVK (GroupVersionKind)**: Strict matching used for validation of API compatibility.
*   **Multi-Stage Pipeline**: Sequential execution of transformation stages where each stage output feeds the next.
*   **Kustomize/Krusty**: The engine used for patching and materializing manifests.
*   **Whiteout Resources**: Filtering resources during the transform pipeline.
*   **Impersonation**: Using `--as` and `--as-extras` flags for handling non-admin migration permissions.
*   **API Surface**: The set of available APIs on a target cluster (often captured as a JSON blob).
*   **Sync Endpoints**: Infrastructure components (Routes, Ingress) used to bridge source and destination clusters for PVC transfers.
*   **Dependency Ordering**: Using `--ordered` to ensure sequential application of resources (e.g., ConfigMaps before Deployments).

## Related Components
*   **Crane Core**: The orchestration engine for the migration pipeline.
*   **Kustomize (embedded)**: Used for resource manipulation.
*   **Kubernetes API/Client-go**: Used for discovery, listing, and applying resources.
*   **Rsync**: The underlying tool utilized by `transfer-pvc` for data migration.
*   **Plugin System**: The architectural extension point for custom migration logic.