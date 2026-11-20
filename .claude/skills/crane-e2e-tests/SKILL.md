---
name: crane-e2e-tests
description: Create, review, or debug Crane E2E tests and CI, including migration scenarios, admin/non-admin runners, direct/indirect PVC transfers, synthetic OpenShift exports, plugin builds, and skipped-test diagnosis.
---

# Crane E2E tests

Apply these repository-specific rules when working on `e2e-tests/` or its CI.
Use `AGENTS.md` and `docs/development/testing.md` for shared verification
commands, and `e2e-tests/README.md` for suite flags and live-test prerequisites.

## Locate the scenario and execution path

Read the nearest scenario and the framework code that supplies its behavior:

| Concern | Source |
| --- | --- |
| Scenario runners and temporary artifact paths | `e2e-tests/framework/scenario.go` |
| Pipeline setup, rendering, and target apply | `e2e-tests/framework/pipeline.go` |
| App commands and Python virtualenv environment | `e2e-tests/framework/app.go` |
| Admin/non-admin namespace setup | `e2e-tests/framework/rbac.go` |
| Transfer mode and inherited suite options | `e2e-tests/framework/crane.go` |

Keep scenarios in the existing tier packages with matching Ginkgo labels.
Discover all `e2e_suite_test.go` files when changing shared flags; updating one
tier bootstrap does not update the others.

## Choose the source and target boundary

- When both clusters support the required APIs, use `NewMigrationScenario`,
  `PrepareSourceApp`, `RunCranePipelineWithChecks`, and the appropriate target
  apply helper from `pipeline.go`.
- For an OpenShift-only source API unavailable on CI minikube, create a realistic
  synthetic export below `paths.ExportDir`. Run `CraneRunner.Transform` and
  `CraneRunner.Apply`, inspect `output.yaml`, and apply the result to the target.
  This tests conversion and target acceptance without a live OpenShift source.

Installing a substitute CRD merely to create an OpenShift built-in input does
not reproduce OpenShift defaulting or admission. Minikube results do not cover
SCC behavior, Routes, or downstream plugin packaging. Use
`KubectlRunner.IsOpenShift()` where assertions depend on the platform and
report any required OpenShift execution separately.

## Select credentials and transfer mode explicitly

- For namespace-admin scenarios, use `SetupActiveKubectlRunners` and register
  its cleanup. It honors `config.RunAs`; calling
  `SetupNamespaceAdminUsersForScenario` directly bypasses the admin override.
- `NewMigrationScenario` maps its `NonAdmin` fields to admin contexts when
  `--run-as=admin` is set. An early `Skip` based on missing raw non-admin
  contexts defeats this behavior. These patterns are checked by
  `.github/workflows/test-compliance.yml`.
- `TransferPVCWithOutput` inherits `config.CloudStorage` when the per-call value
  is empty. A direct-only scenario, such as rsync checksum verification, must
  set `DisableCloudStorage: true` and leave `CloudStorage` empty. That switch
  blocks inheritance; it does not clear an explicitly supplied storage value.
- For indirect scenarios, identify the effective cloud storage and rclone
  configuration. An empty per-call option can still inherit a suite setting.

## Use the existing artifact and app helpers

Create paths with `NewScenarioPaths` and set `runner.WorkDir = paths.TempDir`.
CI uses parallel Ginkgo processes, so isolate namespaces and writable artifacts
between specs. Register cleanup as each resource is allocated.

Keep `K8sDeployApp` for `k8sdeploy deploy`, `validate`, and `remove`. When given
an executable path, it adds that directory to `PATH`, preserving access to
`ansible-playbook` in the same virtualenv.

Package-private helpers can be shared within a suite package. Put reusable
suite helpers in a dedicated helper file; keep spec-specific values near the
spec. Use `framework/` for cluster operations shared across suites and `utils/`
for shared fixture or semantic-comparison code.

Decode rendered resources to check GVK, identity, converted fields, warnings,
and source-resource removal. When output retention or whiteout behavior
changes, update the relevant golden fixtures and existing absence assertions.

## Observe transient resources without cleanup races

- When creation and deletion are both part of the contract, observe each and
  assert the final absence of temporary Secrets, Pods, and transfer helpers.
- Independently deleted resources may never coexist in one polling attempt.
  Retain per-resource observations across attempts, or synchronize observation
  before cleanup, instead of requiring simultaneous presence.
- A PVC may be referenced by Pending or initializing Pods. Match the product's
  applicable non-terminal Pod rule rather than checking only `Running` Pods.
- Use `Eventually` for asynchronous state transitions, including final deletion.

## Carry dependencies through every applicable CI path

Inspect `e2e-pr-tester.yaml` and `e2e-indirect-pr-tester.yaml` under
`.github/workflows/`, plus `.github/actions/run-crane-e2e-tier/action.yml`.
Full, focused, and indirect jobs may set up and launch tests independently.

For plugin scenarios, reuse `.github/actions/build-openshift-plugin/action.yml`
where applicable and select the required plugin revision. Pass its output via
`--plugin-dir`, `config.PluginDir`, and `TransformOptions.PluginDir`. A change
in the composite action alone does not update every focused job. Distinguish
the CI build from registry-installed or downstream-bundled plugins.
Pin an unmerged plugin build to its commit and record that revision.

A local plugin test may skip with a clear reason when its required flag is
absent. CI intended to cover it must supply that dependency and select the spec.

## Use PR CI when no live cluster is accessible

If source or target clusters are unavailable, run the cluster-free checks,
then push the changes and create a PR, or update the existing PR for the task.
Use its GitHub Actions E2E jobs for live validation instead of stopping at
successful compilation.

Wait for the relevant CI jobs to finish. Use `gh` to inspect check results,
failed-job logs, test reports, artifacts, and review feedback. Confirm that
the intended spec executed in the required transfer mode. Investigate failures
and review findings against the current code, push fixes for valid issues,
and inspect the resulting runs again. Preserve the scenario's intended
assertions and required coverage.

Update the PR test plan with run links and actual outcomes. Distinguish test
defects from infrastructure failures or unrelated failures. If CI cannot run
or required platform coverage is unavailable, report that blocker and leave
the affected scenario's live validation explicitly incomplete.

## Establish what actually ran

Use the shared checks in `docs/development/testing.md` and the focused Ginkgo
command in `docs/development/crane-e2e-skill.md#verification-commands`. Keep
build/unit results, suite compilation, and live execution separate.

For live validation, inspect the Ginkgo output or test report for the intended
spec, a non-zero executed-spec count, and its passed/failed/skipped result.
A green job that selected zero specs or skipped the requested scenario does
not validate it. Report the command, platform, admin/non-admin and direct/indirect
modes, plugin revision when relevant, and any skipped scenario with its reason.
