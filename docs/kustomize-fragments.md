# Per-stage kustomize fragments

`crane transform` can merge an inline **kustomize fragment** into the
`kustomization.yaml` that is generated for a stage. This lets you inject extra
kustomize configuration (namespace, common labels/annotations, images, name
prefixes, additional resources/patches, …) without hand-editing the generated
files after every run.

The fragment is provided inline — either through a CLI flag or in the
transform instructions file — following the same per-stage pattern as
[`--stage-optionals`](./multistage-pipeline.md).

## How it works

For each stage, crane generates a `kustomization.yaml` containing `resources`
and `patches`. When a fragment is configured for that stage, it is merged into
the generated file using these rules:

| Field | Merge behaviour |
|-------|-----------------|
| `resources` | Fragment entries are **appended** to the generated ones (de-duplicated by value). |
| `patches` | Fragment entries are **appended** to the generated ones. |
| `apiVersion`, `kind` | Kept from the generated file; fragment values are ignored. |
| any other field | Fragment value **replaces** the generated value. |

Stages without a fragment are left byte-for-byte unchanged.

The stage is identified by its **plugin/base name** (e.g. `KubernetesPlugin`,
`CustomEdits`) — the same key used by `--stage-optionals` — not by the numbered
directory name (`10_KubernetesPlugin`).

## CLI flag

```
--stage-kustomize 'StageName=<YAML or JSON>'
```

The flag is **repeatable** (once per stage). The value after `=` is a kustomize
fragment as a mapping. JSON is valid YAML, so either form works; JSON is usually
easier to pass on a single command line.

### Example: namespace + common labels

```sh
crane transform KubernetesPlugin \
  --stage-kustomize 'KubernetesPlugin={"namespace":"dest-ns","commonLabels":{"app":"crane"}}'
```

Resulting `transform/10_KubernetesPlugin/kustomization.yaml`:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
commonLabels:
  app: crane
namespace: dest-ns
resources:
- input/ConfigMap__v1_default_demo.yaml
```

### Example: image overrides (multi-line YAML value)

```sh
crane transform KubernetesPlugin \
  --stage-kustomize 'KubernetesPlugin=
images:
- name: nginx
  newName: quay.io/mirror/nginx
  newTag: "1.27"
'
```

### Example: multiple stages

```sh
crane transform \
  --stage-kustomize 'KubernetesPlugin={"namespace":"dest-ns"}' \
  --stage-kustomize 'CustomEdits={"commonAnnotations":{"origin":"crane"}}'
```

## Instructions file

Add a `kustomize:` block to a stage entry, alongside the existing `optionals:`
field:

```yaml
# instructions.yaml
stages:
  - name: KubernetesPlugin
    optionals:
      registry-replacement: "docker.io=quay.io"
    kustomize:
      namespace: dest-ns
      commonLabels:
        app: crane
  - name: CustomEdits
    kustomize:
      commonAnnotations:
        origin: instructions
```

Run it with:

```sh
crane transform --instructions-file instructions.yaml
```

> `--instructions-file` cannot be combined with `--stage-kustomize` (or
> `--stage-optionals`). When an instructions file is used, its `kustomize:`
> blocks take precedence.

## Validation & errors

- The fragment must be a **mapping** — a list or scalar is rejected.
- A fragment referencing a stage that is not part of the run fails with
  `per-stage kustomize fragment references unknown stage "..."`.
- `resources`/`patches` in a fragment must be lists.
- Extra `resources` must point to files that exist relative to the stage
  directory, otherwise the subsequent `kustomize build` fails.

## See also

- [Multistage pipeline](./multistage-pipeline.md) — stages, `--stage-optionals`,
  and the instructions file format.
