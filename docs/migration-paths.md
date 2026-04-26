# Migration Paths

Crane is designed to migrate workloads between any conformant Kubernetes clusters. This document describes the supported migration paths and platform-specific considerations.

## Core Capability

Crane's pipeline (export → transform → apply → validate) works with any Kubernetes distribution that exposes standard APIs. The built-in KubernetesPlugin handles the universal concerns: removing server-managed fields, cleaning runtime metadata, and producing clean declarative manifests.

Same-cluster migrations (e.g., migrating between namespaces) are supported for the manifest pipeline. PVC data transfer (`crane transfer-pvc`) between different namespaces on the same cluster is not yet supported but is planned.

## Supported Migration Paths

| Source | Target | Status | Plugins Required |
|--------|--------|--------|-----------------|
| Kubernetes → | Kubernetes | Supported | KubernetesPlugin (built-in) |
| OpenShift 4.x → | OpenShift 4.x | Validated | KubernetesPlugin, OpenshiftPlugin |
| OpenShift 4.x → | Kubernetes | Supported | KubernetesPlugin, OpenshiftPlugin |
| Kubernetes → | OpenShift 4.x | Supported | KubernetesPlugin |

### Kubernetes to Kubernetes

The core migration path. Works across any conformant distributions (e.g., EKS, GKE, AKS, self-managed clusters, kind, minikube).

**What Crane handles:**
- Namespace-scoped resources (Deployments, Services, ConfigMaps, Secrets, PVCs, etc.)
- Related cluster-scoped resources (CRDs, ClusterRoles, ClusterRoleBindings) when RBAC permits
- Persistent volume data transfer via `crane transfer-pvc`

**What you need to handle manually:**
- StorageClass mapping (destination must have compatible storage providers)
- Operator installation (Operators must be pre-installed on the target)
- Ingress controller differences between providers
- Cloud-specific annotations and load balancer configurations

```bash
crane export -n my-app --context source-cluster
crane transform
crane apply
crane validate --context target-cluster
kubectl --context target-cluster apply -f output/output.yaml
```

### OpenShift 4.x to OpenShift 4.x

A validated migration path with dedicated plugin support for OpenShift-specific resources.

**Additional considerations:**
- Routes, ImageStreams, DeploymentConfigs, and SecurityContextConstraints are handled by the OpenshiftPlugin
- SCCs referenced by the namespace workload are migrated when RBAC permits
- Operator-managed resources (Operands) are migrated, but Operators themselves must be pre-installed on the target

```bash
crane export -n my-app --context source-ocp
crane transform
crane transform --stage 20_OpenshiftPlugin
crane apply
crane validate --context target-ocp
kubectl --context target-ocp apply -f output/output.yaml
```

### OpenShift to Kubernetes

Migrating from OpenShift to vanilla Kubernetes requires converting OpenShift-specific resources to their Kubernetes equivalents.

**The OpenshiftPlugin handles:**
- Route → Ingress conversion
- DeploymentConfig → Deployment conversion
- Removal of OpenShift-specific annotations and labels
- ImageStream reference resolution

**What you need to handle manually:**
- SCC equivalents (PodSecurityStandards or PodSecurityPolicies on the target)
- OAuth/authentication differences
- OpenShift-specific Operator replacements

### Kubernetes to OpenShift

Standard Kubernetes resources are natively compatible with OpenShift. The built-in KubernetesPlugin is sufficient for this path. OpenShift-specific features (Routes, SCCs, etc.) can be added via custom transform stages after migration.

## Platform-Specific Plugins

| Plugin | Source | Purpose |
|--------|--------|---------|
| KubernetesPlugin | Built-in (crane-lib) | Removes server-managed fields, universal cleanup |
| OpenshiftPlugin | [crane-plugin-openshift](https://github.com/konveyor/crane-plugin-openshift) | Handles OpenShift-specific resources and conversions |

See [Plugins](./plugins.md) for installation and usage details.

## Limitations

- **PVC transfer on same cluster** — `crane transfer-pvc` does not support transferring PVC data between namespaces on the same cluster (the manifest pipeline works fine for same-cluster migrations)
- **Cross-version API migration** (e.g., `v1beta1` → `v1`) requires custom transform stages or manual manifest updates
- **Operator-managed resources** are migrated as static manifests; reconciliation by the Operator on the target cluster may modify them
- **Namespace-scoped by design** — Crane focuses on namespace workloads; cluster-wide infrastructure migration is out of scope

See [Resource Compatibility](./resource-compatibility.md) for detailed resource type support.

## Further Reading

- [Resource Compatibility](./resource-compatibility.md) — Which resource types Crane migrates
- [Stateless Migration Tutorial](./stateless-migration-tutorial.md) — Step-by-step walkthrough
- [Stateful Migration Tutorial](./stateful-migration-tutorial.md) — Migrating applications with PVCs
- [Troubleshooting](./troubleshooting.md) — Common issues and solutions
