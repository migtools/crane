# COMMANDS Documentation Index

## Overview
This documentation area defines the command-line interface for `crane`, a Kubernetes migration tool designed to facilitate the movement of workloads between clusters. It covers the full lifecycle of a migration pipeline: exporting resources, transforming them via plugins, applying them with Kustomize, validating them against cluster APIs, and migrating stateful PersistentVolume data.

## Files Summary
*   **commands/export.md**: Details the process of discovering and writing Kubernetes objects and CRDs from a source cluster to local YAML files.
*   **commands/transform.md**: Explains the multi-stage pipeline for modifying exported manifests using Kustomize patches and custom/plugin-based transformation stages.
*   **commands/validate.md**: Describes the procedure for checking that final, transformed manifests are compatible with the target cluster's API surface, supporting both live and offline modes.
*   **commands/apply.md**: Covers the usage of the embedded Kustomize engine to render the final manifest set, including options for dependency-ordered output and non-admin migrations.
*   **commands/transfer-pvc.md**: Details the mechanism for moving PersistentVolumeClaim resources and their underlying data between clusters using rsync-based pods and network endpoints.

## Code Changes That Would Require Documentation Updates
*   **Flag Additions/Removals**: Any changes to CLI flags, default values, or flag types across any `crane` command.
*   **Pipeline Architecture**: Changes to the sequential flow of data between `export`, `transform`, `apply`, and `validate` (e.g., changes to internal directory structures).
*   **Transformation Logic**: Modifications to the plugin discovery mechanism, stage naming conventions (`<number>_<name>`), or the precedence rules for `Plugin` vs. "Pass-through" stages.
*   **API/GVK Handling**: Changes to how CRDs are collected during export, or how `crane validate` performs GVK matching against the target cluster API.
*   **Kustomize Integration**: Upgrades or changes to the embedded `krusty` API, or changes to how `kustomization.yaml` is generated/interpreted.
*   **RBAC/Impersonation**: Updates to how `crane` handles `Forbidden` errors, namespace verification, or the `--as` impersonation flags.
*   **PVC Transfer Mechanism**: Changes to the rsync pod orchestration, new supported endpoint types (e.g., LoadBalancer), or modifications to the PVC/Namespace mapping logic.

## Key Technical Concepts
*   **GVK (Group/Version/Kind)**: The core mechanism for matching and validating resource compatibility.
*   **Multi-Stage Pipeline**: The sequential execution of transformations where the output of one stage acts as the input for the next.
*   **Kustomize / Krusty**: The underlying engine used for manifest patching and resource rendering.
*   **Namespace-scoped vs. Cluster-scoped Resources**: The distinction in handling resources like `ConfigMap` versus `ClusterRole`.
*   **Whiteout/Filtering**: The removal or exclusion of specific resources during the transformation process.
*   **Impersonation**: The use of `--as` and `--as-extras` flags for handling non-admin migrations.
*   **rsync-based Data Transfer**: The method used by `transfer-pvc` to migrate block storage data between source and destination pods.
*   **API Surface Capture**: The process of generating an offline `api-surface.json` for validation in air-gapped environments.

## Related Components
*   **`crane` Binary**: The main entry point and CLI controller.
*   **Kustomize (embedded)**: The processing engine for manifest transformations.
*   **Plugins**: External or internal logic hooks used during the `transform` phase.
*   **Kubeconfig**: The authentication and cluster-connectivity provider.
*   **Target/Source Clusters**: The Kubernetes environments acting as origin and destination.
*   **Rsync Daemon/Client**: The temporary pods used for data synchronization during `transfer-pvc`.