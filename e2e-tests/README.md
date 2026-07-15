# E2E Test Framework

This directory contains the end-to-end (E2E) test suite for validating cross-cluster migration flows with the `crane` CLI.

The suite is built with Ginkgo/Gomega and is organized to keep scenario code readable while reusing framework helpers for command execution, setup, and cleanup.

## Directory Layout

- `config/config.go`
  - Shared runtime configuration values populated from test flags (paths, kube contexts, verbose mode).
- `tests/tier0/e2e_suite_test.go`
  - Ginkgo suite entrypoint for tier0 tests.
  - Registers CLI flags and configures test logging.
- `tests/tier1/e2e_suite_test.go`
  - Ginkgo suite entrypoint for tier1 tests.
  - Registers CLI flags and configures test logging.
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
    - RBAC permission checks (`kubectl auth can-i`)
    - deployment scaling with label-first and fallback behavior
- `framework/client.go`
  - `client-go` helpers for Kubernetes API lookups and kubeconfig context/user resolution.
- `framework/pipeline.go`
  - Reusable migration pipeline helpers (run crane stages, verify artifacts, apply to target, prepare source app).
  - Includes admin and non-admin apply paths (`ApplyOutputToTarget`, `ApplyOutputToTargetNonAdmin`).
- `framework/scenario.go`
  - Shared scenario object construction (admin and non-admin runners), temp path management, and standardized cleanup.
- `framework/rbac.go`
  - Namespace-admin setup helpers for non-admin contexts:
    - grant/revoke namespace admin rolebinding
    - scenario-level setup for source and target clusters
    - RBAC preflight checks
- `testdata/rolebinding_namespace_admin.yaml`
  - RoleBinding template used by namespace-admin setup helpers.
- `framework/logging.go`
  - Centralized verbose command/output logging helpers.
- `utils/utils.go`
  - Generic test utility functions (temp directory creation, recursive file listing, file presence checks).

## Test Organization

Tests are organized into two tiers under `tests/`:

- `tests/tier0/` — must-have gate tests; run on every push to main and nightly. These cover core migration scenarios that must pass before a release.
- `tests/tier1/` — extended coverage; run nightly. These cover more advanced or environment-specific scenarios.

Each tier is a separate Go package with its own `e2e_suite_test.go` entrypoint. Tests are also tagged with a matching Ginkgo label (`Label("tier0")` or `Label("tier1")`) so they can be filtered independently.

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

For namespace-admin scenarios, tests additionally:

1. Use non-admin contexts (`source-nonadmin-context` and `target-nonadmin-context`).
2. Grant namespace-scoped admin rolebindings using framework RBAC helpers.
3. Apply manifests with `ApplyOutputToTargetNonAdmin(...)` (namespace already created via RBAC setup).

## Running the Tests

Run all tiers from repo root:

```bash
ginkgo run -v --recurse e2e-tests/tests -- \
  --k8sdeploy-bin=/path/to/k8sdeploy \
  --crane-bin=/path/to/crane \
  --source-context=src \
  --target-context=tgt \
  --source-nonadmin-context=src-dev \
  --target-nonadmin-context=tgt-dev \
  --verbose-logs
```

Run only tier0:

```bash
ginkgo run -v --recurse --label-filter="tier0" e2e-tests/tests -- \
  --k8sdeploy-bin=/path/to/k8sdeploy \
  --crane-bin=/path/to/crane \
  --source-context=src \
  --target-context=tgt \
  --source-nonadmin-context=src-dev \
  --target-nonadmin-context=tgt-dev \
  --verbose-logs
```

Run only tier1:

```bash
ginkgo run -v --recurse --label-filter="tier1" e2e-tests/tests -- \
  --k8sdeploy-bin=/path/to/k8sdeploy \
  --crane-bin=/path/to/crane \
  --source-context=src \
  --target-context=tgt \
  --source-nonadmin-context=src-dev \
  --target-nonadmin-context=tgt-dev \
  --verbose-logs
```

Run a single spec by focus:

```bash
ginkgo run -v --recurse --focus="\[MTC-329\]" e2e-tests/tests -- \
  --k8sdeploy-bin=/path/to/k8sdeploy \
  --crane-bin=/path/to/crane \
  --source-context=src \
  --target-context=tgt \
  --source-nonadmin-context=src-dev \
  --target-nonadmin-context=tgt-dev
```

## Flags

Defined in `tests/tier0/e2e_suite_test.go` and `tests/tier1/e2e_suite_test.go`:

- `--k8sdeploy-bin` path to `k8sdeploy` executable
- `--crane-bin` path to `crane` executable
- `--source-context` source kube context
- `--target-context` target kube context
- `--source-nonadmin-context` source kube context for namespace-admin (non-cluster-admin) user flows
- `--target-nonadmin-context` target kube context for namespace-admin (non-cluster-admin) user flows
- `--verbose-logs` enable command and output logging for framework runners
- `--run-as` set to `admin` to run all tests with cluster-admin credentials. Omit this flag to
run tests in their default mode (non-admin tests as non-admin, admin tests as admin).

## Adding a New Scenario

For consistency, prefer this structure:

1. Use `NewMigrationScenario(...)` to initialize runners and app handles.
2. Use `NewScenarioPaths(...)` for temp directories.
3. Register `DeferCleanup(...)` with `CleanupScenario(...)` early.
4. Reuse framework helpers:
   - `PrepareSourceApp(...)`
   - `RunCranePipelineWithChecks(...)`
   - `ApplyOutputToTarget(...)` for admin flows
   - `ApplyOutputToTargetNonAdmin(...)` for namespace-admin flows
   - `SetupNamespaceAdminUsersForScenario(...)` for non-admin RBAC setup
5. Place the file in `tests/tier0/` or `tests/tier1/` depending on its tier, and tag the `It()` block with the matching `Label("tier0")` or `Label("tier1")`.
6. Keep test-specific assertions and scenario-specific logic in the test file.

This keeps scenario files focused on behavior, while framework files handle command plumbing and shared orchestration.
