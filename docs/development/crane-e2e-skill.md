# Using the Crane E2E Claude Code skill

The project skill at `.claude/skills/crane-e2e-tests/SKILL.md` gives Claude Code repository-specific instructions for creating, changing, reviewing, and debugging Crane end-to-end tests.

It covers:

- Ginkgo tier organization and labels
- `k8s-apps-deployer` and `k8sdeploy`
- Crane framework helpers
- admin/non-admin runner selection and inherited transfer settings
- minikube and OpenShift differences
- plugin builds and `--plugin-dir`
- fixtures and golden output
- resource lifecycle assertions
- CI workflow integration
- local and cluster-backed verification
- distinguishing executed scenarios from skipped or unselected specs

## Getting the skill

The skill is stored in the repository. Team members receive it when they check out a revision that contains:

```text
.claude/skills/crane-e2e-tests/SKILL.md
```

Start Claude Code from the Crane repository root:

```bash
cd /path/to/crane
claude
```

Restart Claude Code after pulling a new or changed skill. Claude Code loads project skills when a session starts.

No global installation or personal configuration is required.

The root `AGENTS.md` also directs agents working on E2E changes to read this
skill. Codex and pi can follow the same file as repository instructions without
using Claude Code's native skill command.

## Invoking the skill

Claude Code can select the skill automatically when a request clearly concerns Crane e2e tests. Include concrete terms such as `e2e-tests`, `Ginkgo`, `tier0`, `tier1`, `k8sdeploy`, or `plugin-dir` in the request.

Example:

```text
Add a tier1 e2e test under e2e-tests for migration of a DeploymentConfig.
Use a custom OpenShiftPlugin build and validate the rendered Deployment on the target cluster.
```

To request the skill explicitly, invoke it by name:

```text
/crane-e2e-tests
```

Then describe the task in the same message or the next message.

## Recommended requests

### Add a scenario

Provide the behavior, source and target platforms, expected tier, and external dependencies.

```text
Use /crane-e2e-tests to add tier1 coverage for converting an OpenShift
DeploymentConfig to a Kubernetes Deployment. The source API is unavailable on
minikube, so use a synthetic export fixture. Apply and inspect the result on the
target cluster.
```

### Change an existing test

Name the test and describe the product behavior that changed. Ask Claude Code to inspect related framework helpers and golden manifests before editing.

```text
Use /crane-e2e-tests to update MTA-875 because PVCs are no longer whiteouted.
Check all tests and golden manifests that assume the PVC is absent from output.
```

### Review a change

Ask for a code review rather than a rewrite. Include the PR number when review discussion or CI context matters.

```text
Use /crane-e2e-tests to review PR #1234. Focus on cleanup races, cross-platform
behavior, target-side assertions, helper reuse, and CI paths. Do not post review
comments.
```

### Debug a failure

Provide the exact command, failing spec, cluster type, and relevant output.

```text
Use /crane-e2e-tests to debug this tier1 failure on OpenShift. Determine whether
the cause is SCC admission, the application fixture, the Crane pipeline, or a
different OpenShiftPlugin version. Reproduce the narrowest part locally first.
```

### Add plugin-dependent coverage

Identify the repository and immutable commit for an unmerged plugin change.

```text
Use /crane-e2e-tests to test crane-plugin-openshift commit <sha>. Reuse the CI
plugin build action and pass its output through --plugin-dir in applicable
full, focused, and indirect jobs. Confirm the requested scenario executes.
```

## Information to include

The agent produces better results when the request states:

- The behavior or regression being tested
- The related issue, test case, or PR
- Whether the test belongs in tier0 or tier1
- Whether source and target are minikube, OpenShift, or mixed
- Whether non-admin contexts are required
- Whether PVC data transfer is direct or indirect
- Any required plugin repository and commit
- The expected resources and final cluster state
- Which cluster-backed checks are available locally

If these details are unknown, ask Claude Code to inspect existing scenarios and recommend the closest pattern before editing.

## Expected workflow

When the skill is active, Claude Code should:

1. Read `e2e-tests/README.md`, nearby tests, and the framework wrappers involved in the scenario.
2. Check full, focused, and indirect CI paths when introducing a flag or dependency.
3. Choose between a live source application and a synthetic export fixture.
4. Reuse scenario and pipeline helpers. Select admin/non-admin runners through `SetupActiveKubectlRunners` and make the transfer mode explicit when the behavior requires it.
5. Keep tests in the existing tier packages and use Ginkgo labels for selection.
6. Add assertions for generated artifacts and target-cluster state.
7. Account for asynchronous creation and deletion without adding fixed sleeps.
8. Run the shared build, unit-test, E2E compilation, and formatting commands, followed by the focused Ginkgo scenario when its environment is available.
9. If clusters are unavailable, create or update the task's PR and wait for its E2E CI. Analyze failures and review feedback, fix valid issues, and inspect follow-up runs.
10. Report compile and unit results separately from live execution, including whether the requested scenario ran, passed, failed, or skipped.

## Reviewing the result

Before accepting generated changes, verify:

- The test uses the correct tier label.
- Cleanup is registered before later setup can fail.
- Namespaces and temporary paths are unique.
- `Eventually` does not require independently deleted resources to coexist.
- PVC usage checks include applicable non-terminal Pods, not only `Running` Pods.
- A lifecycle test verifies both transient and final states.
- Package-private helpers may be shared within a suite. Cross-suite cluster operations are in `framework/`, and shared fixture or comparison code is in `utils/`.
- Non-admin tests respect `--run-as=admin` through the active-runner helpers and do not skip based only on missing raw non-admin contexts.
- Direct-only transfer tests disable suite-wide cloud storage inheritance explicitly.
- Golden manifests compare semantic content and do not capture unstable metadata.
- OpenShift-specific behavior has an OCP test result when minikube cannot cover it.
- Full, focused, and indirect CI jobs receive new flags and dependencies where needed.
- Ginkgo output or reports confirm that the requested scenario executed; a green job with only skipped or unselected specs is not validation.
- The final response states which tests actually ran and which require a cluster.

## Verification commands

Run the shared checks from the repository root before cluster-backed execution:

```bash
make build
make test-unit
make test-e2e-compile
make fmt-check
```

`make test-unit` includes E2E config, framework, and utility packages but
excludes live suites. `make test-e2e-compile` recursively compiles every suite
package without executing tests or benchmarks, including any additional tiers.
Discover all current suite bootstrap files when adding a shared flag.

Use `GO_TEST_ARGS` for test flags such as `-v` or `-race`. For CI-compatible
unit coverage:

```bash
make test-unit GO_TEST_ARGS=-v COVERAGE_PROFILE=coverage.out COVERAGE_MODE=atomic
```

During iteration, a focused package check such as
`go test ./e2e-tests/framework ./e2e-tests/utils` is useful. A bare
`go test ./...` also runs the live Ginkgo suites and is not a cluster-free
unit check. Report unit and compilation results separately from live execution.

`make fmt-check` does not modify files. Format changed files with
`gofmt -w path/to/changed.go` and report unrelated existing differences
separately.

Install the pinned Ginkgo CLI as described in the
[testing guide](testing.md#running-e2e-tests), then prepare the clusters,
contexts, and dependencies required by the scenario.

The following example selects an existing tier0 ConfigMap test. Replace
`TEST_FILE` with the actual file for your scenario. The suite directory is
derived from that file, and `--fail-on-empty` fails if the focus filter selects
no specs in that suite:

Use clusters containing only disposable test workloads. This example's cleanup
deletes the fixed `mta-810-configmap` namespace on both source and target,
including any resources already present there. Before running it, confirm that
`mta-810-configmap` does not exist on either cluster.

```bash
TEST_FILE=e2e-tests/tests/tier0/mta_810_configmap_test.go
TEST_SUITE="$(dirname "$TEST_FILE")"
test -f "$TEST_FILE"
ginkgo run -v --fail-on-empty --focus-file="$TEST_FILE" "$TEST_SUITE" -- \
  --k8sdeploy-bin=k8sdeploy \
  --crane-bin="$PWD/crane" \
  --source-context=src \
  --target-context=tgt \
  --source-nonadmin-context=src-dev \
  --target-nonadmin-context=tgt-dev \
  --verbose-logs
```

Add scenario-specific flags such as `--plugin-dir`, `--cloud-storage`, or `--rclone-config-file` only when required.

Inspect the Ginkgo result for the requested spec and a non-zero executed-spec
count. Report its status, platform, active admin/non-admin and transfer modes,
and plugin revision when relevant. Explain any skipped scenario separately.

### When clusters are unavailable

After the cluster-free checks, push the changes and create a PR, or update
the existing task PR. Use its E2E CI for live validation and wait for the
relevant jobs to complete.

Inspect CI logs, reports, artifacts, and review feedback. Check that the
requested spec actually executed, address valid failures and findings, and
inspect the next CI run after pushing fixes. Record run links and outcomes
in the PR test plan. If CI or required platform coverage is unavailable,
report the blocker and mark that live validation as incomplete.

## Background

The lifecycle and PVC-use rules reflect reviews of [#974](https://github.com/migtools/crane/pull/974),
[#918](https://github.com/migtools/crane/pull/918), and
[#927](https://github.com/migtools/crane/pull/927). Cross-platform and output
expectations also draw on [#1009](https://github.com/migtools/crane/pull/1009),
[#1015](https://github.com/migtools/crane/pull/1015),
[#991](https://github.com/migtools/crane/pull/991),
[#992](https://github.com/migtools/crane/pull/992),
[#941](https://github.com/migtools/crane/pull/941), and
[#952](https://github.com/migtools/crane/pull/952).

Helper organization follows reviews of [#923](https://github.com/migtools/crane/pull/923),
[#948](https://github.com/migtools/crane/pull/948),
[#958](https://github.com/migtools/crane/pull/958),
[#959](https://github.com/migtools/crane/pull/959), and
[#897](https://github.com/migtools/crane/pull/897).

## Maintaining the skill

Update `.claude/skills/crane-e2e-tests/SKILL.md` when the suite changes its directory layout, flags, framework abstractions, CI topology, or external dependencies.

Keep this document focused on team usage. Put detailed implementation rules in the skill itself so Claude Code receives them during execution.
