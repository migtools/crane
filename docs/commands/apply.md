# crane apply

Apply transformations to exported resources and produce final manifests.

## Synopsis

```bash
crane apply [flags]
```

## Description

`crane apply` runs `kubectl kustomize` on each transform stage to apply patches, producing clean, declarative YAML ready for deployment to a target cluster. By default, all stages are applied sequentially to maintain consistency across the pipeline. The output includes both a single multi-document YAML file and individual resource files organized by namespace.

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--export-dir` | `-e` | `export` | The path where exported resources are saved (kept for consistency; not used by apply) |
| `--transform-dir` | `-t` | `transform` | The path where transform stage directories are located |
| `--output-dir` | `-o` | `output` | The path where final manifests are written |
| `--stage` | | | Apply a specific stage only (e.g., `10_KubernetesPlugin`). If not specified, all stages are applied |

## Output Structure

```text
output/
├── output.yaml                      # All resources in a single multi-document YAML
└── resources/                       # Individual resource files
    └── <namespace>/
        ├── Deployment_apps_v1_<ns>_<name>.yaml
        ├── Service__v1_<ns>_<name>.yaml
        └── ConfigMap__v1_<ns>_<name>.yaml
```

- **`output.yaml`** — Ready for `kubectl apply -f`
- **`resources/<namespace>/`** — Individual files for selective review or application

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
crane apply --stage 10_KubernetesPlugin
```

### Deploy to target cluster

```bash
crane apply
kubectl apply -f output/output.yaml
```

## Prerequisites

`crane apply` requires `kubectl` to be installed and available in your `$PATH`. It uses `kubectl kustomize` internally to process each stage's `kustomization.yaml`.

## Common Errors

| Error | Cause | Solution |
|-------|-------|----------|
| `kubectl not found` | kubectl is not installed or not in PATH | Install kubectl and ensure it is in your PATH |
| `kustomization.yaml validation failed` | Invalid Kustomize syntax or missing resource files | Run `kubectl kustomize transform/<stage>/` manually to see detailed errors |
| `invalid stage name` | Stage name doesn't follow `<number>_<name>` format | Use a valid stage name like `10_KubernetesPlugin` |

## Next Steps

After applying, validate the manifests against your target cluster:

```bash
crane validate --input-dir output --context target-cluster
```

See [crane validate](./validate.md) for details. Or deploy directly:

```bash
kubectl apply -f output/output.yaml
```
