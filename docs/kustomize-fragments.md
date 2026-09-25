# Declarative transformations with an instructions file

An instructions file defines the transform stages, their order, plugin options,
and Kustomize settings in one YAML file. Store this file with the migration
manifests to make the transform pipeline reviewable and repeatable.

Use a `kustomize` block when a stage needs settings that a transform plugin does
not provide, such as a target namespace, image replacement, labels, or an
inline patch. Crane merges the block into the `kustomization.yaml` generated for
that stage and applies it before the next stage runs.

## Define the pipeline

The following file runs the Kubernetes plugin first and then a pass-through
stage with application-specific changes:

```yaml
# instructions.yaml
stages:
  - name: KubernetesPlugin
    optionals:
      registry-replacement: "docker.io=quay.io"
    kustomize:
      namespace: destination
      labels:
        - pairs:
            migration.konveyor.io/managed-by: crane
      images:
        - name: nginx
          newName: quay.io/example/nginx
          newTag: "1.27"
  - name: ApplicationSettings
    kustomize:
      patches:
        - target:
            group: apps
            version: v1
            kind: Deployment
            name: web
          patch: |-
            - op: replace
              path: /spec/replicas
              value: 3
```

Each item under `stages` accepts these fields:

| Field | Purpose |
|-------|---------|
| `name` | Plugin name or pass-through stage name. |
| `optionals` | Options passed only to that stage's plugin. |
| `kustomize` | Kustomize fields merged into that stage's generated file. |

A name ending in `Plugin` requires a matching plugin. Other names create
pass-through stages. Crane assigns directory names from the list order:

```text
transform/
|-- 10_KubernetesPlugin/
`-- 20_ApplicationSettings/
```

The instructions file uses base names such as `KubernetesPlugin`, not generated
directory names such as `10_KubernetesPlugin`.

## Run the transformation

Export resources, then pass the file to `crane transform`:

```sh
crane export --export-dir export
crane transform \
  --export-dir export \
  --transform-dir transform \
  --instructions-file instructions.yaml
```

Crane processes the stages in file order. Each stage reads the applied output
of the preceding stage. In the example, `ApplicationSettings` receives
resources that already have the namespace, labels, and image replacement from
`KubernetesPlugin`.

Inspect the generated files or render the final manifests:

```sh
crane apply --transform-dir transform --output-dir output
```

## Repeat a transformation

The instructions file remains the source of the generated stage configuration.
After changing it, regenerate the stages with `--overwrite`:

```sh
crane transform \
  --export-dir export \
  --transform-dir transform \
  --instructions-file instructions.yaml \
  --overwrite
```

`--overwrite` replaces existing stage directories. Do not keep manual changes
inside a generated stage if they are not represented by the instructions file
or another reproducible input.

If `transform/` contains a stage that is absent from the instructions file,
Crane reports the difference. With `--overwrite`, Crane removes the extra stage
and makes the directory match the declared stage list.

## Merge rules

Crane first generates each stage's `kustomization.yaml`, then merges its
`kustomize` block using these rules:

| Field | Result |
|-------|--------|
| `resources` | Append entries and remove duplicate string values. |
| `patches` | Append entries. |
| `apiVersion`, `kind` | Keep Crane's generated values. |
| Any other field | Replace the generated field with the declared value. |

A stage without a `kustomize` block keeps the standard generated output.

Prefer inline patch content, as shown above. A path in `resources` or
`patches.path` must exist when Kustomize builds the stage. Stage regeneration
removes files kept manually inside that stage, so local referenced files are
not suitable as undeclared inputs to a repeatable instructions-file workflow.

## CLI alternative

For an ad hoc run without an instructions file, use the repeatable
`--stage-kustomize` flag:

```sh
crane transform KubernetesPlugin \
  --stage-kustomize 'KubernetesPlugin={"namespace":"destination"}'
```

The format is `StageName=YAML_OR_JSON`. Use one flag per stage. JSON is often
easier to quote on a single command line.

`--instructions-file` cannot be combined with positional stage arguments,
`--stage-optionals`, or `--stage-kustomize`.

## Validation

Crane rejects an instructions file when:

- it has no stages;
- stage names are empty, duplicated, or contain unsupported characters;
- a stage entry contains a field other than `name`, `optionals`, or
  `kustomize`;
- a Kustomize fragment references a stage outside the configured pipeline;
- `resources` or `patches` is not a list.

Kustomize reports schema errors and missing referenced resources while building
the affected stage.

## Related documentation

- [Multi-stage pipeline](./multistage-pipeline.md)
- [`crane transform` command](./commands/transform.md)
