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

### Offline Validation

Use `--api-resources` to validate offline against a captured API surface JSON file when the target cluster is not directly reachable. This is mutually exclusive with `--context`, `--kubeconfig`, `--server`, `--token`, `--cluster`, and `--user`.

#### Capturing the API Surface

Run the following script against the target cluster to capture its API surface for offline validation:

```bash
#!/bin/bash
# capture-api-surface.sh
# Usage: capture-api-surface.sh [-o output.json] [--context name] [--kubeconfig path]
OUTPUT="api-surface.json"
KUBECTL_FLAGS=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    -o) OUTPUT="$2"; shift 2 ;;
    --context|--kubeconfig) KUBECTL_FLAGS="$KUBECTL_FLAGS $1=$2"; shift 2 ;;
    *) echo "Unknown flag: $1"; exit 1 ;;
  esac
done

kubectl api-versions $KUBECTL_FLAGS | while read gv; do
  endpoint=$([ "$gv" = "v1" ] && echo "/api/v1" || echo "/apis/$gv")
  kubectl get --raw "$endpoint" $KUBECTL_FLAGS 2>/dev/null || true
done | jq -s '{"apiResourceLists":.}' > "$OUTPUT"
```

Or as a one-liner:

```bash
kubectl api-versions | while read gv; do kubectl get --raw $([ "$gv" = "v1" ] && echo "/api/v1" || echo "/apis/$gv") 2>/dev/null || true; done | jq -s '{"apiResourceLists":.}' > api-surface.json
```

Then use the captured file for offline validation:

```bash
crane validate --api-resources api-surface.json
```

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--input-dir` | `-i` | `output` | Path to the apply output directory containing final manifests |
| `--validate-dir` | | `validate` | Path where validation results and failures are saved |
| `--output` | `-o` | `json` | Report file format: `json` or `yaml` |
| `--api-resources` | | | Path to API surface JSON file for offline validation (mutually exclusive with `--context`/`--kubeconfig`/`--server`/`--token`/`--cluster`/`--user`) |
| `--overwrite` | | `false` | Overwrite the validate directory if it already exists |

Standard kubeconfig flags (`--kubeconfig`, `--context`, `--cluster`, etc.) are also available to specify the target cluster for live validation.

## Output Structure

```text
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

### Validate against current context (live mode)

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

### Offline validation against captured API surface

```bash
crane validate --api-resources api-surface.json --input-dir output
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
| `loading kubeconfig` | Cannot connect to target cluster | Check kubeconfig and `--context` flag, or use `--api-resources` for offline mode |
| `--api-resources and --context are mutually exclusive` | Both offline and live flags specified | Use one mode or the other. `--api-resources` is also mutually exclusive with `--kubeconfig`, `--server`, `--token`, `--cluster`, and `--user` |
| `validate directory "X" already exists` | Validate directory from a previous run | Use `--overwrite` to replace it |
| Validation failures | GVKs not available on target cluster | Install required CRDs/operators on target, or transform manifests to use supported API versions |

## Next Steps

If validation passes, deploy to the target cluster:

```bash
kubectl apply -f output/output.yaml
```

If validation fails, review the report and failures directory, then adjust your transform stages or target cluster configuration.
