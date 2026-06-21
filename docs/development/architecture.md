# Crane Architecture

Crane follows a pipeline architecture designed around Unix philosophy: small, focused tools assembled in powerful ways. All operations are non-destructive and output results to disk.

## Pipeline Overview

```text
Source Cluster → export → transform → apply → validate → Target Cluster
                  │           │          │         │
                  ▼           ▼          ▼         ▼
              export/     transform/   output/   validate/
```

Each stage reads from disk and writes to disk, making the pipeline transparent, auditable, and repeatable.

## Export Phase

**Package:** `cmd/export/` and `internal/`

1. **Discovery** — Queries the Kubernetes API server for all available resource types using the discovery client
2. **Listing** — Lists all resources in the specified namespace, respecting label selectors and pagination
3. **RBAC filtering** — Identifies cluster-scoped resources (ClusterRoleBindings, ClusterRoles, SCCs) related to ServiceAccounts in the namespace
4. **CRD collection** — Exports CRD definitions for custom resources found in the namespace; operator-managed CRDs (identified via owner references) are skipped
5. **Writing** — Writes individual YAML files to `export/resources/<namespace>/`

Key design decisions:
- Uses `dynamic.Interface` for listing any resource type without compile-time knowledge
- Failures are recorded to `export/failures/` rather than aborting the entire export
- Cluster-scoped resources go to a `_cluster/` subdirectory
- When CRD access is denied, a warning is logged and export continues (assumes CRDs exist on target)

## Transform Phase

**Package:** `cmd/transform/` and `internal/transform/`

The transform phase uses a multi-stage Kustomize pipeline:

1. **Stage discovery** — Scans `transform/` for existing stage directories matching `<number>_<name>` pattern
2. **Plugin execution** — For plugin-based stages (name ending in `Plugin`), loads and runs the matching plugin to generate JSONPatch operations
3. **Resource writing** — Writes individual resource files to `<stage>/input/`
4. **Patch writing** — Writes plugin-generated patches to `<stage>/patches/`
5. **Kustomization generation** — Generates `kustomization.yaml` linking resources and patches

### Stage Types

- **Plugin stages** (`10_KubernetesPlugin`): Automatically regenerated on each run
- **Custom stages** (`30_CustomEdits`): Pass-through stages for manual editing, protected from overwrites without `--force`

### Sequential Consistency

In multi-stage pipelines, each stage runs on the fully materialized output of the previous stage (not raw patches). Each stage contains `input/`, `patches/`, and `output/` directly within the stage directory (e.g., `transform/<stage>/input/`, `transform/<stage>/output/`).

### Key Components

- **`Orchestrator`** (`internal/transform/orchestrator.go`): Coordinates multi-stage execution, manages stage discovery, plugin loading, and sequential consistency
- **`Writer`** (`internal/transform/writer.go`): Handles writing resources, patches, and kustomization files to stage directories
- **`Stage`** (`internal/transform/stages.go`): Represents a transform stage with discovery, validation, and navigation utilities

## Apply Phase

**Package:** `cmd/apply/` and `internal/apply/`

1. **Stage discovery** — Discovers all stages in the transform directory
2. **Kustomize build** — Runs embedded kustomize (via the `krusty` API from `sigs.k8s.io/kustomize`) on each stage's directory
3. **Cluster-scoped filtering** — When `--skip-cluster-scoped` is set, filters out cluster-scoped resources from output
4. **Output writing** — Writes results to `output/output.yaml` (combined) and `output/resources/<namespace>/` (individual files); cluster-scoped resources go to `output/resources/_cluster/`

The `KustomizeApplier` (`internal/apply/kustomize.go`) embeds the kustomize library directly via the `krusty.MakeKustomizer` API, eliminating the external kubectl dependency. Additional kustomize arguments (e.g., `--enable-helm`) can be passed via `--kustomize-args`.

## Validate Phase

**Package:** `cmd/validate/` and `internal/validate/`

1. **Scanning** (`scanner.go`): Reads manifests from the output directory, extracting GVK + namespace tuples
2. **Matching** (`matcher.go`): Queries the target cluster's discovery API to check if each GVK is served; alternatively, matches against a captured API surface JSON file for offline validation
3. **Reporting** (`report.go`): Generates a compatibility report (JSON/YAML) and writes incompatible resources to a failures directory

Supports two modes:
- **Live mode**: Queries target cluster via kubeconfig/context
- **Offline mode**: Validates against a captured API surface file (`--api-resources`), useful for air-gapped environments

## Plugin System

Plugins are external binaries that:
1. Receive a Kubernetes resource on stdin
2. Return JSONPatch operations on stdout
3. Are discovered from `~/.local/share/crane/plugins/` (default)

The built-in `KubernetesPlugin` (from `crane-lib`) removes server-managed fields like `metadata.uid`, `metadata.resourceVersion`, `metadata.creationTimestamp`, `metadata.managedFields`, and `status`.

## PVC Transfer

**Package:** `cmd/transfer-pvc/`

Uses the [pvc-transfer](https://github.com/backube/pvc-transfer) library:
1. Creates a PVC on the destination cluster
2. Sets up an rsync daemon Pod with an encrypted stunnel transport
3. Creates a public endpoint (Route or Ingress) on the destination
4. Runs an rsync client Pod on the source to transfer data
5. Cleans up all transfer resources after completion

## Data Flow

```text
                    ┌──────────────┐
                    │ Source       │
                    │ Cluster      │
                    └──────┬───────┘
                           │ crane export
                           ▼
                    ┌──────────────┐
                    │ export/      │  Raw manifests (namespace + _cluster/)
                    │ resources/   │
                    └──────┬───────┘
                           │ crane transform
                           ▼
                    ┌──────────────┐
                    │ transform/   │  Kustomize stages with patches
                    │ <stages>/    │
                    └──────┬───────┘
                           │ crane apply (embedded kustomize)
                           ▼
                    ┌──────────────┐
                    │ output/      │  Clean, deployable manifests
                    │ output.yaml  │
                    └──────┬───────┘
                           │ crane validate
                           ▼
                    ┌──────────────┐
                    │ validate/    │  Compatibility report
                    │ report.json  │
                    └──────┬───────┘
                           │ kubectl apply
                           ▼
                    ┌──────────────┐
                    │ Target       │
                    │ Cluster      │
                    └──────────────┘
```
