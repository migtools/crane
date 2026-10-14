# COMMANDS Documentation Index

## Overview
This documentation area defines the operational interface for the Crane migration tool, covering the end-to-end workflow of exporting, transforming, applying, and validating Kubernetes resources. It provides technical specifications for command-line arguments, directory structures, and data handling procedures for cross-cluster migrations and PVC transfers.

## Files Summary
* **commands/apply.md**: Describes how to use the `crane apply` command to run Kustomize stages and generate declarative YAML for cluster deployment.
* **commands/export.md**: Explains the `crane export` command used to extract resource manifests and CRDs from a source cluster.
* **commands/transfer-pvc.md**: Details the `crane transfer-pvc` command for migrating PersistentVolumeClaim data using direct or indirect (S3/cloud storage) transport modes.
* **commands/transform.md**: Provides guidance on the `crane transform` command, including plugin-based stage management, patch application, and pipeline architecture.
* **commands/validate.md**: Documents the `crane validate` command for ensuring rendered manifests are compatible with the target cluster's API surface.

## Code Changes That Would Require Documentation Updates
* **Flag Additions/Modifications:** Adding new CLI flags, changing default values, or deprecating flags (e.g., changing `--output` format options).
* **Command Logic/Flow:** Changes to the sequential order of the pipeline, new stages (e.g., "Pre-flight check" commands), or modifications to how `crane apply` manages Kustomize (e.g., switching from `krusty` API to an external `kubectl` dependency).
* **Output Structure:** Changes to the directory hierarchy created by `export`, `transform`, or `apply`, or changes to file naming conventions (e.g., altering the resource prefixing logic).
* **Plugin Architecture:** Changes to the `transform` stage naming conventions or how "Plugin" vs "Pass-through" stages are identified.
* **Transfer/Sync Logic:** Updates to rsync/rclone behavior, changes to ownership normalization logic (UID/GID), or the addition of new transport protocols for PVC migration.
* **Audit Logging:** Any change to the default path of `audit/.crane-audit.log` or the internal format of the audit logs.
* **Authentication/Authorization:** New methods for impersonation or changes to how `crane` handles RBAC limitations for non-admin migrations.

## Key Technical Concepts
* **Pipeline Architecture:** Export -> Transform -> Apply -> Validate.
* **GVK (Group, Version, Kind):** The foundation for validation and resource identification.
* **Kustomize:** The underlying technology for resource transformation.
* **PVC Transfer Modes:** Direct (rsync via ingress/route) and Indirect (S3-compatible via rclone).
* **Sequential Consistency:** The dependency-preserving nature of transformation stages.
* **Audit Logging:** Structured JSON Lines logging for operational traceability.
* **Impersonation:** Kubeconfig-based user/group impersonation during export.
* **Cluster-Scoped Resources:** Handling of CRDs, ClusterRoles, and ClusterRoleBindings.
* **StorageClass Conversion:** Mapping requirements for cross-provider PVC migration.

## Related Components
* **Kustomize (Krusty API):** Embedded manifest modification engine.
* **Rclone:** Backend tool for indirect PVC data migration.
* **Rsync:** Backend tool for direct PVC data synchronization.
* **Kubernetes API Server:** Target for resource discovery and validation.
* **Audit Logging Subsystem:** Global logging mechanism integrated across all commands.
* **Local Workspace Manager:** Handles the `export/`, `transform/`, `output/`, and `validate/` directory structures.