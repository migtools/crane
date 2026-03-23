# E2E Test Framework

This directory contains the end-to-end (E2E) test suite for validating cross-cluster migration flows with the `crane` CLI.

The suite is built with Ginkgo/Gomega and is organized to keep scenario code readable while reusing framework helpers for command execution, setup, and cleanup.

## Directory Layout

- `config/config.go`
  - Shared runtime configuration values populated from test flags (paths, kube contexts, verbose mode).
- `tests/e2e_suite_test.go`
  - Ginkgo suite entrypoint.
  - Registers CLI flags and configures test logging.
- `tests/stateless_migration_test.go`
  - Stateless migration scenario(s), including export/transform/apply and target validation.
- `tests/stateful_migration_test.go`
  - Stateful migration scenario for PVC-aware flow and `transfer-pvc`.
- `framework/app.go`
  - Wrapper around `k8sdeploy` app lifecycle commands: deploy, validate, remove.
  - Automatically prepends `dirname(k8sdeploy-bin)` to `PATH` when binary path is explicit, so dependent tools (like `ansible-playbook`) are resolvable.
- `framework/crane.go`
  - Wrapper around `crane` commands: export, transform, apply, transfer-pvc.
- `framework/kubectl.go`
  - Wrapper around `kubectl` actions used in tests:
    - namespace creation
    - dry-run apply validation
    - recursive apply
    - deployment scaling with label-first and fallback behavior
- `framework/client.go`
  - `client-go` helpers for Kubernetes API lookups (PVC listing and node IP resolution).
- `framework/pipeline.go`
  - Reusable migration pipeline helpers (run crane stages, verify artifacts, apply to target, prepare source app).
- `framework/scenario.go`
  - Shared scenario object construction, temp path management, and standardized cleanup.
- `framework/logging.go`
  - Centralized verbose command/output logging helpers.
- `utils/utils.go`
  - Generic test utility functions (temp directory creation, recursive file listing, file presence checks).

## How the Suite Works

At a high level, each migration test follows the same pattern:

1. Build scenario runners (`k8sdeploy`, `crane`, `kubectl`) using source and target contexts.
2. Deploy and validate source app.
3. Quiesce source workload (scale down where needed).
4. Run crane pipeline:
   - `export`
   - `transform`
   - `apply` (render output manifests)
5. Validate generated artifacts.
6. Apply rendered manifests on target cluster.
7. Scale target workload and validate app behavior.
8. Cleanup source/target app resources and temp artifacts.

Stateful scenarios add PVC discovery and `transfer-pvc` steps between pipeline generation and target validation.

## Running the Tests

From repo root:

```bash
ginkgo run -v e2e/tests -- \
  --k8sdeploy-bin=/path/to/k8sdeploy \
  --crane-bin=/path/to/crane \
  --source-context=src \
  --target-context=tgt \
  --verbose-logs
```

Run a single spec by focus:

```bash
ginkgo run -v --focus="\[MTC-329\]" e2e/tests -- \
  --k8sdeploy-bin=/path/to/k8sdeploy \
  --crane-bin=/path/to/crane \
  --source-context=src \
  --target-context=tgt
```

## Flags

Defined in `tests/e2e_suite_test.go`:

- `--k8sdeploy-bin` path to `k8sdeploy` executable
- `--crane-bin` path to `crane` executable
- `--source-context` source kube context
- `--target-context` target kube context
- `--verbose-logs` enable command and output logging for framework runners

## Adding a New Scenario

For consistency, prefer this structure:

1. Use `NewMigrationScenario(...)` to initialize runners and app handles.
2. Use `NewScenarioPaths(...)` for temp directories.
3. Register `DeferCleanup(...)` with `CleanupScenario(...)` early.
4. Reuse framework helpers:
   - `PrepareSourceApp(...)`
   - `RunCranePipelineWithChecks(...)`
   - `ApplyOutputToTarget(...)`
5. Keep test-specific assertions and scenario-specific logic in the `tests/` file.

This keeps scenario files focused on behavior, while framework files handle command plumbing and shared orchestration.
