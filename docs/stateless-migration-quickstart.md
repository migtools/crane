# Quickstart Stateless Migration Tutorial

This quickstart covers a generic stateless migration workflow in Crane using the current pipeline:

`export -> transform -> apply -> validate`

## Prerequisites

- `crane` CLI installed
- Kubernetes access configured for source and target contexts
- A namespace in the source cluster containing the resources you want to migrate

Before running any Crane commands, make sure your local `kubeconfig` already includes valid contexts for both clusters. Crane runs locally and uses the `--context` flag to talk directly to each cluster using your existing Kubernetes RBAC permissions.

## 1) Set Environment Variables

```bash
export SOURCE_CONTEXT=src-cluster
export TARGET_CONTEXT=tgt-cluster
export SOURCE_NAMESPACE=source-namespace
```

## 2) Export

`crane export` discovers resources in the source namespace and writes them as Kubernetes manifest files on disk.

Run export with an explicit export directory:

- `-e export` tells Crane to write exported manifests under the `export/` directory.

```bash
crane export \
  --context "${SOURCE_CONTEXT}" \
  -n "${SOURCE_NAMESPACE}" \
  -e export
```

Example output snippet (abbreviated):

```text
INFO[0000] adding resource: secrets to the list of GVRs to be extracted
INFO[0000] adding resource: services to the list of GVRs to be extracted
INFO[0000] adding resource: deployments to the list of GVRs to be extracted
INFO[0000] No matching cluster-scoped resources found; _cluster/ directory will be empty
INFO[0000] Writing objects of resource: secrets to the output directory
INFO[0000] Writing objects of resource: services to the output directory
INFO[0000] Writing objects of resource: deployments to the output directory
```

What you can expect to see (example):

- Log lines showing resources discovered for extraction (for example, `secrets`, `services`, `configmaps`, `deployments`)
- A write phase with lines like `Writing objects of resource: <name> to the output directory`
- If no cluster-scoped objects match, a message that `_cluster/` will be empty
- Exported manifests written under `export/resources/${SOURCE_NAMESPACE}/`
- If extraction fails for specific objects, failure artifacts are written under `export/failures/${SOURCE_NAMESPACE}/`

## 3) Transform (Default Stage)

`crane transform` takes exported manifests (output from `crane export`), cleans and updates them in stages, and saves the results in `transform/`.

A stage is one step in the transform pipeline.

Think of it like an assembly line:
- Stage 1 takes your exported manifests and makes the first set of changes.
- Stage 2 takes Stage 1 output and applies the next changes.

Example: `10_KubernetesPlugin` runs first, then `25_CustomStage` runs on top of that result.

Run transform with explicit input and stage directories:

- `-e export` tells Crane to read exported resources from the `export/` directory.
- `-t transform` tells Crane to write and execute transform stages under the `transform/` directory.

```bash
crane transform -e export -t transform
```

Example output snippet (abbreviated):

```text
INFO[0000] No existing stages found, creating default stages for 1 plugin(s)
INFO[0000] Creating default stage for plugin: KubernetesPlugin -> 10_KubernetesPlugin
INFO[0000] Created 1 default stage(s): [10_KubernetesPlugin]
INFO[0000] Populating and executing all default stages
INFO[0000] Executing stage 1/1: 10_KubernetesPlugin
INFO[0000] Stage 10_KubernetesPlugin: loaded 10 input resource(s)
INFO[0000] Stage 10_KubernetesPlugin: produced 4 output resource(s)
INFO[0000] Successfully completed 1 stage(s)
```

What you can expect to see (example):

- Default stage creation logs when no stages exist yet
- Stage execution progress (for example, `Executing stage 1/1: 10_KubernetesPlugin`)
- Input and output resource counts per stage
- A success line indicating all stages completed
- Stage artifacts in `transform/10_KubernetesPlugin/`: `input/`, `patches/`, `output/`, `kustomization.yaml`

Stage naming uses numeric prefixes to control order. For example, `10_KubernetesPlugin` runs before `25_CustomStage`, and Crane executes stages from lowest number to highest number.

Expected transform directory shape (example):

```text
transform/
└── 10_KubernetesPlugin
    ├── input/
    │   ├── ...ConfigMap...
    │   ├── ...Deployment...
    │   ├── ...Secret...
    │   └── ...Service...
    ├── kustomization.yaml
    ├── output/
    │   └── <source-namespace>/
    │       ├── ...ConfigMap...
    │       ├── ...Deployment...
    │       ├── ...Secret...
    │       └── ...Service...
    └── patches/
        ├── ...Deployment.patch.yaml
        ├── ...ConfigMap.patch.yaml
        ├── ...Secret.patch.yaml
        └── ...Service.patch.yaml
```

## 4) Optional: Add Additional Stages

You can add a custom pass-through stage:

```bash
crane transform -e export -t transform 25_CustomStage
```

What you should see (example):

- `transform/25_CustomStage/` with `input/`, `output/`, `kustomization.yaml`

Custom stages are rendered with Kustomize. After editing resources in your custom stage, you can preview the rendered manifests before `crane apply`:

```bash
kubectl kustomize transform/25_CustomStage
```

In most pipelines, your custom stage is the last stage under `transform/`, so rendering that directory should show the manifests that will feed into the final apply output.

For a deeper explanation of stage ordering, stage structure, and multi-stage behavior, see [Multi-Stage Kustomize Transform Pipeline](./multistage-pipeline.md).

If a custom stage already contains edits, rerun with `--force` to regenerate the custom stage directories and custom stage artifacts under `transform/` (for example: `input/`, `output/`, `kustomization.yaml`):

```bash
crane transform -e export -t transform --force
```

## 5) Apply (Render Final Manifests)

`crane apply` renders the final manifests from transform stages into deployable output files.

Run apply with explicit transform and output directories:

- `-t transform` tells Crane which staged transform directory to read.
- `-o output` tells Crane where to write the final rendered manifests.

```bash
crane apply -t transform -o output
```

Example output snippet (abbreviated):

```text
INFO[0000] Applying all stages...
INFO[0000] Applying final stage: 10_KubernetesPlugin
INFO[0000] Successfully applied final stage to .../output/output.yaml
```

What you can expect to see (example):

- `output/output.yaml` generated as one combined file containing all rendered resources (useful for a single `kubectl apply -f`).
- `output/resources/` generated as separate files per resource (useful when you want to review, diff, or apply resources selectively).

For namespace-only/non-admin scenarios:

```bash
crane apply \
  --skip-cluster-scoped
```

## 6) Validate (Optional, Recommended)

`crane validate` checks whether the rendered manifests are compatible with the target cluster API.

Run live validation against the target cluster API. This step is optional, but strongly recommended before promoting manifests across environments:

- `-i output` tells Crane which rendered manifest directory to validate.
- `--validate-dir validate` tells Crane where to write validation reports and failure artifacts.

```bash
crane validate \
  --context "${TARGET_CONTEXT}" \
  -i output \
  --validate-dir validate
```

Example output snippet (abbreviated):

```text
INFO[0000] Scanned 3 distinct GVK+namespace tuples
INFO[0000] Validating in live mode against context "tgt"
Mode: live (context: tgt)
...
Summary: 3 scanned, 3 compatible, 0 incompatible
Result: PASSED — all resources compatible with target cluster
INFO[0000] Wrote validation report to validate/report.json
```

What you can expect to see (example):

- Terminal output includes `Mode: live` and a compatibility summary/result
- For successful validation, `Result: PASSED` with all scanned resources marked compatible
- Validation report generated at `validate/report.json`
- If incompatibilities are found, failure artifacts are written under `validate/failures/`

Example report structure:

```json
{
  "mode": "live",
  "clusterContext": "tgt-cluster",
  "results": [
    {
      "apiVersion": "apps/v1",
      "kind": "Deployment",
      "namespace": "target-namespace",
      "resourcePlural": "deployments",
      "status": "OK"
    },
    {
      "apiVersion": "v1",
      "kind": "Secret",
      "namespace": "target-namespace",
      "resourcePlural": "secrets",
      "status": "OK"
    },
    {
      "apiVersion": "v1",
      "kind": "Service",
      "namespace": "target-namespace",
      "resourcePlural": "services",
      "status": "OK"
    }
  ],
  "totalScanned": 3,
  "compatible": 3,
  "incompatible": 0
}
```

## 7) Optional: Instructions-File Driven Transform

For repeatable pipelines, drive stage behavior with an instructions file:

Example `instructions.yaml`:

```yaml
stages:
  - KubernetesPlugin
  - CustomStage
```

```bash
crane transform \
  --instructions-file ./instructions.yaml
```

What you should see (example):

- Stage directories defined by the instructions file are created in `transform/`
- Transform runs exactly the stages listed in `instructions.yaml`, in the same order they are provided

## 8) Apply Cleaned Manifests to Target Cluster

After validation passes and you are satisfied with the cleaned manifests, apply them to the target cluster:

```bash
kubectl --context "${TARGET_CONTEXT}" apply -f output/output.yaml
```

Make sure the target namespace already exists before applying manifests, so namespace-scoped resources do not fail during apply.

## Troubleshooting

### Export directory already exists

Use `--overwrite` with `crane export`.

### Apply or validate directory already exists

Use `--overwrite` with `crane apply` or `crane validate`.

### Existing custom stage blocks rerun

Use `crane transform --force` to regenerate stage directories.

### Validation shows incompatibilities

- Check `validate/report.json` for `apiVersion`/`kind` mismatches
- Update transforms, then rerun `crane apply` and `crane validate`
