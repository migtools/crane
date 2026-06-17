# crane apply

Apply transformations to exported resources and produce final manifests.

## Synopsis

```bash
crane apply [stage...] [flags]
```

## Description

`crane apply` runs embedded kustomize on each transform stage to apply patches, producing clean, declarative YAML ready for deployment to a target cluster. By default, all stages are applied sequentially to maintain consistency across the pipeline. The output includes both a single multi-document YAML file and individual resource files organized by namespace.

Kustomize is embedded directly in the Crane binary (via the krusty API), so no external `kubectl` dependency is needed.

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--export-dir` | `-e` | `export` | The path where exported resources are saved (kept for consistency; not used by apply) |
| `--transform-dir` | `-t` | `transform` | The path where transform stage directories are located |
| `--output-dir` | `-o` | `output` | The path where final manifests are written |
| `--kustomize-args` | | | Additional arguments for kustomize (e.g., `--enable-helm --helm-command=helm3`) |
| `--skip-cluster-scoped` | | `false` | Exclude cluster-scoped resources (ClusterRole, ClusterRoleBinding, CRD, etc.) from output. Useful for non-admin migration scenarios |
| `--overwrite` | | `false` | Overwrite the output directory if it already exists |

Stages are specified as positional arguments (e.g., `crane apply 10_KubernetesPlugin`). Stages can be specified by directory name or plugin name. If no stages are specified, all discovered stages are applied sequentially.

## Output Structure

```text
output/
├── output.yaml                      # All resources in a single multi-document YAML
└── resources/                       # Individual resource files
    ├── <namespace>/
    │   ├── Deployment_apps_v1_<ns>_<name>.yaml
    │   ├── Service__v1_<ns>_<name>.yaml
    │   └── ConfigMap__v1_<ns>_<name>.yaml
    └── _cluster/                    # Cluster-scoped resources (when not skipped)
        ├── ClusterRoleBinding_rbac.authorization.k8s.io_v1_clusterscoped_<name>.yaml
        └── ClusterRole_rbac.authorization.k8s.io_v1_clusterscoped_<name>.yaml
```

- **`output.yaml`** — Ready for `kubectl apply -f`
- **`resources/<namespace>/`** — Individual files for selective review or application
- **`resources/_cluster/`** — Cluster-scoped resources (omitted when `--skip-cluster-scoped` is set)

## Examples

### Apply all stages (default)

```bash
crane apply
```

### Apply with custom directories

```bash
crane apply --transform-dir ./migration/transform --output-dir ./migration/output
```

### Apply a specific stage only

```bash
crane apply 10_KubernetesPlugin
```

### Skip cluster-scoped resources (non-admin migration)

When migrating without cluster-admin privileges, use `--skip-cluster-scoped` to exclude ClusterRoles, ClusterRoleBindings, CRDs, and other cluster-scoped resources from the output. These resources typically require admin permissions to create on the target cluster.

```bash
crane apply --skip-cluster-scoped
```

For a complete non-admin migration pipeline:

```bash
crane export -n my-app                    # Skips inaccessible resources gracefully
crane transform
crane apply --skip-cluster-scoped         # Excludes cluster-scoped resources
crane validate --context target-cluster   # Or use --api-resources for offline
```

### Pass additional kustomize arguments

```bash
crane apply --kustomize-args "--enable-helm --helm-command=helm3"
```

### Deploy to target cluster

```bash
crane apply
kubectl apply -f output/output.yaml
```

## Common Errors

| Error | Cause | Solution |
|-------|-------|----------|
| `kustomization.yaml validation failed` | Invalid Kustomize syntax or missing resource files | Run `crane apply <stage>` to isolate the failing stage |
| `output directory "X" already exists` | Output directory from a previous run | Use `--overwrite` to replace it |
| `invalid stage name` | Stage name doesn't follow `<number>_<name>` format | Use a valid stage name like `10_KubernetesPlugin` |
| `invalid kustomize-args` | Unsupported or malformed kustomize arguments | Check supported kustomize flags |

## Next Steps

After applying, validate the manifests against your target cluster:

```bash
crane validate --input-dir output --context target-cluster
```

See [crane validate](./validate.md) for details. Or deploy directly:

```bash
kubectl apply -f output/output.yaml
```
