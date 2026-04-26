# crane validate

Validate final manifests against a target cluster's API surface.

## Synopsis

```bash
crane validate [flags]
```

## Description

`crane validate` checks the final rendered manifests (from `crane apply`'s output) for compatibility with a target cluster. It verifies that every `apiVersion` + `kind` combination is served by the target cluster's API surface using strict GVK matching.

This is the final step in the Crane migration pipeline: **export → transform → apply → validate**.

Incompatible resources are written to a `failures/` directory under the validate-dir for auditability.

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--input-dir` | `-i` | `output` | Path to the apply output directory containing final manifests |
| `--validate-dir` | | `validate` | Path where validation results and failures are saved |
| `--output` | `-o` | `json` | Report file format: `json` or `yaml` |

Standard kubeconfig flags (`--kubeconfig`, `--context`, `--cluster`, etc.) are also available to specify the target cluster.

## Output Structure

```
validate/
├── report.json           # (or report.yaml) Full validation report
└── failures/             # Only created if incompatible resources found
    └── <resource-files>
```

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | All checks pass — all GVKs are available on the target cluster |
| `1` | One or more checks failed, or another error occurred |

## Examples

### Validate against current context

```bash
crane validate
```

### Validate against a specific target cluster

```bash
crane validate --context target-cluster
```

### Validate with custom input directory

```bash
crane validate --input-dir ./migration/output
```

### Generate YAML report

```bash
crane validate --output yaml
```

### Full pipeline example

```bash
crane export -n my-app
crane transform
crane apply
crane validate --context target-cluster
```

## Common Errors

| Error | Cause | Solution |
|-------|-------|----------|
| `input-dir "X" is not a directory` | Path doesn't exist or isn't a directory | Run `crane apply` first to generate output |
| `loading kubeconfig` | Cannot connect to target cluster | Check kubeconfig and `--context` flag |
| Validation failures | GVKs not available on target cluster | Install required CRDs/operators on target, or transform manifests to use supported API versions |

## Next Steps

If validation passes, deploy to the target cluster:

```bash
kubectl apply -f output/output.yaml
```

If validation fails, review the report and failures directory, then adjust your transform stages or target cluster configuration.
