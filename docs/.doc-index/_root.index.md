# _ROOT Documentation Index

## Overview
This documentation area provides comprehensive guidance on using **Crane**, a non-destructive Kubernetes migration tool. It covers the end-to-end migration pipeline (export, transform, apply, validate, and transfer), plugin architecture, storage class conversion, and best practices for pre-migration validation.

## Files Summary
- **README.md**: Serves as the central navigation hub, providing an overview of the Crane CLI, its core pipeline concepts, and links to installation, commands, and development guides.
- **installation.md**: Details prerequisites, download/build instructions, verification steps, and a basic test to ensure the Crane binary is functional.
- **migration-tutorial.md**: Offers a step-by-step walkthrough of migrating a sample stateful application, covering the full pipeline including PVC transfer and verification.
- **multistage-pipeline.md**: Explains the architecture and usage of Crane’s multi-stage Kustomize-based transform pipeline, including stage directory structures and plugin priority logic.
- **plugins.md**: Documents the plugin system, including built-in (Kubernetes) and community (OpenShift) plugins, plugin management, and instructions for passing configuration flags.
- **pre-apply-validation-guide.md**: Provides a security and stability checklist for validating manifests before applying them to a target cluster, including dry-run and RBAC verification.
- **resource-compatibility.md**: Outlines the compatibility boundaries for namespace-scoped vs. cluster-scoped resources, including warnings on namespace renaming and RBAC/SCC requirements.
- **storageclass-conversion.md**: Describes workflows for migrating PersistentVolumeClaim data to different StorageClasses, including rename-mapping strategies and cross-cluster transfer options.

## Code Changes That Would Require Documentation Updates
- **CLI Command Signatures:** Any changes to flags, arguments, or the order of operations in the pipeline (export/transform/apply/validate/transfer-pvc).
- **Pipeline Logic:** Alterations to the Kustomize transformation sequence, stage naming conventions, or the "dirty check" mechanism in the transform package.
- **Plugin Interface:** Changes to the plugin binary contract, input/output formats, or the method for passing optional configuration flags.
- **Artifact/Directory Structure:** Any changes to the `transform/` stage directory naming, the `_cluster/` export directory, or the `output/` structure.
- **Default/Built-in Behavior:** Modifications to what the `KubernetesPlugin` automatically strips (e.g., adding or removing metadata fields).
- **Network/Security Logic:** Changes to the `transfer-pvc` mechanism (e.g., adding new endpoint types like S3 or changing ingress/route requirements).
- **RBAC/Permission Requirements:** Updates to the roles or service account requirements necessary for performing migrations or interacting with cluster-scoped resources.
- **StatefulSet/Volume Handling:** Changes to how `pvc-rename-map` or `volumeClaimTemplates` are processed during the transform/apply phase.

## Key Technical Concepts
- **Migration Pipeline:** Export, Transform, Apply, Validate, and Transfer-PVC.
- **Kustomize Pipeline:** Sequential, stage-based transformation using numeric priority prefixes (e.g., `10_KubernetesPlugin`).
- **JSONPatch:** The primary mechanism used by plugins to clean and adapt Kubernetes resources.
- **Resource Compatibility:** Handling of Namespace-scoped vs. Cluster-scoped resources (CRDs, ClusterRoles, SCCs).
- **Transfer-PVC:** Rsync-based data migration using stunnel, supporting direct, indirect (S3), and rename-based transfers.
- **Dry-Run Validation:** `kubectl apply --dry-run=server` and `kubectl auth can-i` checks for pre-migration safety.
- **Stage Chaining:** Sequential execution logic where the output of one stage acts as the input for the next.
- **Dirty Check:** Protection mechanism preventing the overwrite of manually modified stage directories.

## Related Components
- **crane-lib:** The library containing the core logic and built-in Kubernetes plugin.
- **Kustomize:** The underlying engine used for resource modification and assembly.
- **OpenShift/Kubernetes API Servers:** The target environments for cluster compatibility and validation.
- **Rsync:** The utility driving the `transfer-pvc` mechanism.
- **Stunnel:** Used for encrypted PVC data transfer during migration.
- **Operator Lifecycle Manager (OLM):** Relevant for target cluster dependency readiness (Operators).