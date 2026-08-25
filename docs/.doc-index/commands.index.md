# COMMANDS Documentation Index

## Overview
This documentation area defines the command-line interface and operational workflows for `crane`, a Kubernetes migration tool. It covers the end-to-end migration pipeline, including resource discovery, transformation, application, validation, and data-plane transfer of persistent volumes.

## Files Summary
* **commands/export.md**: Explains the discovery and export of Kubernetes resources (including CRDs and cluster-scoped RBAC) from a source cluster to a local directory.
* **commands/transform.md**: Details the process of applying Kustomize-based transformations and plugin-driven modifications to exported manifests.
* **commands/apply.md**: Describes the process of rendering final manifests using embedded Kustomize and organizing them for deployment, including options for dependency ordering.
* **commands/validate.md**: Outlines how to verify the compatibility of transformed manifests against a target cluster’s API surface, supporting both live and offline modes.
* **commands/transfer-pvc.md**: Details the mechanism for migrating PersistentVolumeClaim resources and their underlying data between clusters using rsync-based pods.

## Code Changes That Would Require Documentation Updates
* **Flag/Argument Modifications**: Adding, removing, or changing defaults for any CLI flags (e.g., changing `--qps` in `export` or adding new transformation options).
* **Output Structure Changes**: Any changes to the directory hierarchy of `export/`, `transform/`, or `output/` folders.
* **Plugin Architecture**: Updates to how transformation plugins are discovered, prioritized, or the introduction of new plugin lifecycle stages.
* **API Version Requirements**: Changes to the validation logic or supported API surface identification methods in `crane validate`.
* **Impersonation Logic**: Updates to `--as` or `--as-extras` handling in `crane export` for non-admin migrations.
* **Transfer Protocol**: Changes to the rsync implementation, certificate generation, or supported endpoint types (e.g., adding `loadbalancer` or `nodeport` support) in `transfer-pvc`.
* **Kustomize Integration**: Changes to the embedded `krusty` library usage or how `kustomization.yaml` files are generated/managed.

## Key Technical Concepts
* **GVK (Group/Version/Kind)**: Used for API surface matching and resource identification.
* **Kustomization Pipeline**: The sequential execution of transform stages using JSON patches.
* **Whiteout Resources**: Files tracked during transformation but explicitly excluded from active deployment.
* **Sequential Consistency**: The requirement that transformation stages operate on the materialized output of previous stages.
* **Multi-Stage Pipelines**: The ability to chain plugins (`*Plugin`) and manual pass-through stages.
* **Dependency-Ordered Deployment**: The `--ordered` flag functionality for `kubectl apply`.
* **Rsync Daemon/Client Architecture**: The data-plane mechanism for `transfer-pvc`.
* **API Surface Capture**: The offline validation workflow involving JSON-based API resource definitions.

## Related Components
* **Kustomize (embedded)**: The core engine for resource modification and manifest generation.
* **Kubernetes API Server**: The primary interaction point for `export` and `validate`.
* **CRD Controllers**: The source of CustomResourceDefinitions managed during the migration pipeline.
* **Rsync/Pod Execution**: The data-plane subsystem used for volume transfer.
* **RBAC/Impersonation Subsystem**: Handles authentication context for non-admin cluster migration.