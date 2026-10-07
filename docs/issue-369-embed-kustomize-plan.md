# Issue #369: Embed Kustomize into Crane

## Context

Crane's `transform` and `apply` commands shell out to `kubectl kustomize` (or `oc kustomize`) via `exec.Command` to build kustomization stages. This creates an external dependency — users must have `kubectl` or `oc` installed. Issue #369 requests embedding kustomize as a Go library (`sigs.k8s.io/kustomize/api/krusty`) to make crane fully self-contained.

The `krusty` API is already an indirect dependency in `go.mod` (`sigs.k8s.io/kustomize/api v0.20.1`). The API provides `krusty.MakeKustomizer(opts).Run(fSys, path)` which returns a `ResMap` that can be serialized to YAML via `AsYaml()`.

## Scope

- Remove all `exec.Command` calls to `kubectl`/`oc` for kustomize from production code
- Preserve all existing kustomize args support (helm, load-restrictor, alpha-plugins)
- Update `crane version` to show kustomize library version
- E2E test framework keeps using `kubectl` for cluster operations (out of scope)

## Current State: exec.Command Locations


| File                                     | Function                     | What it does                                                                                         |
| ---------------------------------------- | ---------------------------- | ---------------------------------------------------------------------------------------------------- |
| `internal/apply/kustomize.go:139`        | `runKustomizeBuild()`        | `exec.Command(kustomizeCmd, "kustomize", ...args, dir)` — main apply kustomize call                  |
| `internal/transform/orchestrator.go:313` | `applyStageTransforms()`     | `exec.Command(kustomizeCmd, "kustomize", ...args, stageDir)` — transform inter-stage materialization |
| `internal/file/file_helper.go:216-224`   | `GetKustomizeCommand()`      | Tries `kubectl version --client`, falls back to `oc version --client`                                |
| `internal/apply/kustomize.go:157-163`    | `ValidateKubectlAvailable()` | Same kubectl/oc detection as above                                                                   |


---

## Phase 1: Core — Replace exec.Command with krusty API

### Step 1.1: Create embedded kustomize runner

**File**: `internal/kustomize/runner.go` (new)

Create a `Runner` struct that wraps the krusty API:

```go
type Runner struct {
    Log  *logrus.Logger
    Args []string // parsed kustomize args from --kustomize-args
}

func (r *Runner) Build(dir string) ([]byte, error)
```

The `Build` method:

1. Creates `krusty.Options` by mapping `Args` to the appropriate fields (see mapping table below)
2. Creates `krusty.MakeKustomizer(opts)`
3. Calls `Run(filesys.MakeFsOnDisk(), dir)`
4. Returns `resMap.AsYaml()`

### Step 1.2: Replace exec.Command in orchestrator (transform)

**File**: `internal/transform/orchestrator.go`

In `applyStageTransforms()`: replace the `exec.Command` block with `kustomize.Runner{}.Build(stageDir)`. Keep the existing YAML → unstructured parsing code.

### Step 1.3: Replace exec.Command in applier (apply)

**File**: `internal/apply/kustomize.go`

In `runKustomizeBuild()`: replace the `exec.Command` block with `kustomize.Runner{}.Build(dir)`.

### Step 1.4: Remove GetKustomizeCommand and ValidateKubectlAvailable

- `internal/file/file_helper.go` — remove `GetKustomizeCommand()`
- `internal/apply/kustomize.go` — remove `ValidateKubectlAvailable()`
- `cmd/apply/apply.go` — remove `ValidateKubectlAvailable()` call
- `internal/transform/test_helpers.go` — update `hasKustomizeCommand()` to always return true

### Step 1.5: Update `--kustomize-args` comment

**File**: `internal/kustomize/args.go` — update comment from "ready for exec.Command" to "for kustomize configuration".

---

## Phase 2: Version and Cleanup

### Step 2.1: Update `ok l version`

- `internal/buildinfo/buildinfo.go` — add `KustomizeVersion` 
- `cmd/version/version.go` — add kustomize version to output

### Step 2.2: Update go.mod

- Promote `sigs.k8s.io/kustomize/api` from indirect to direct
- Run `go mod tidy`

---

## Phase 3: Testing

### Step 3.1: Unit tests for runner

**File**: `internal/kustomize/runner_test.go` (new)

- `TestBuild_BasicKustomization`
- `TestBuild_WithPatches`
- `TestBuild_WithLoadRestrictionsNone`
- `TestBuild_WithHelm`
- `TestBuild_InvalidDir`

### Step 3.2: Existing tests

All existing tests in `internal/transform/` and `internal/apply/` should pass — output format (multi-doc YAML bytes) is unchanged.

### Step 3.3: Manual test

Run pipeline with kubectl hidden from PATH to prove independence.

---

## Arg Mapping Reference


| CLI Arg                                      | krusty.Options Field                   | Value                            |
| -------------------------------------------- | -------------------------------------- | -------------------------------- |
| `--load-restrictor=LoadRestrictionsNone`     | `opts.LoadRestrictions`                | `types.LoadRestrictionsNone`     |
| `--load-restrictor=LoadRestrictionsRootOnly` | `opts.LoadRestrictions`                | `types.LoadRestrictionsRootOnly` |
| `--enable-helm`                              | `opts.PluginConfig.HelmConfig.Enabled` | `true`                           |
| `--helm-command <cmd>`                       | `opts.PluginConfig.HelmConfig.Command` | `cmd`                            |
| `--enable-alpha-plugins`                     | `opts.PluginConfig.PluginRestrictions` | `types.PluginRestrictionsNone`   |
| `--env KEY=VAL` / `-e KEY=VAL`               | `os.Setenv()` before build             | environment variable             |


---

## Files to Create/Modify


| File                                 | Action                                                                  |
| ------------------------------------ | ----------------------------------------------------------------------- |
| `internal/kustomize/runner.go`       | **New**: Embedded kustomize runner using krusty API                     |
| `internal/kustomize/runner_test.go`  | **New**: Unit tests for embedded runner                                 |
| `internal/kustomize/args.go`         | **Modify**: Update comment                                              |
| `internal/transform/orchestrator.go` | **Modify**: Replace exec.Command with kustomize.Runner                  |
| `internal/apply/kustomize.go`        | **Modify**: Replace runKustomizeBuild + remove ValidateKubectlAvailable |
| `internal/file/file_helper.go`       | **Modify**: Remove GetKustomizeCommand                                  |
| `internal/transform/test_helpers.go` | **Modify**: Update hasKustomizeCommand                                  |
| `cmd/apply/apply.go`                 | **Modify**: Remove ValidateKubectlAvailable call                        |
| `cmd/version/version.go`             | **Modify**: Add kustomize version                                       |
| `internal/buildinfo/buildinfo.go`    | **Modify**: Add KustomizeVersion                                        |
| `go.mod`                             | **Modify**: Promote kustomize/api to direct, tidy                       |


