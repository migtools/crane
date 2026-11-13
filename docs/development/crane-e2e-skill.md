# Using the Crane E2E Claude Code skill

The project skill at `.claude/skills/crane-e2e-tests/SKILL.md` gives Claude Code repository-specific instructions for creating, changing, reviewing, and debugging Crane end-to-end tests.

It covers:

- Ginkgo tier organization and labels
- `k8s-apps-deployer` and `k8sdeploy`
- Crane framework helpers
- minikube and OpenShift differences
- plugin builds and `--plugin-dir`
- fixtures and golden output
- resource lifecycle assertions
- CI workflow integration
- local and cluster-backed verification

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
Use /crane-e2e-tests to test crane-plugin-openshift commit <sha>. Add a reusable
CI build action, pass its output through --plugin-dir in full and focused jobs,
and make the test skip clearly when the local flag is absent.
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
4. Reuse `NewMigrationScenario`, `NewScenarioPaths`, pipeline helpers, and cleanup helpers where applicable.
5. Keep tests in the existing tier packages and use Ginkgo labels for selection.
6. Add assertions for generated artifacts and target-cluster state.
7. Account for asynchronous creation and deletion without adding fixed sleeps.
8. Run formatting, framework and utility tests, suite compilation, and the focused Ginkgo scenario when its environment is available.
9. Report compile and unit results separately from cluster-backed execution.

## Reviewing the result

Before accepting generated changes, verify:

- The test uses the correct tier label.
- Cleanup is registered before later setup can fail.
- Namespaces and temporary paths are unique.
- `Eventually` does not require independently deleted resources to coexist.
- PVC usage checks include applicable non-terminal Pods, not only `Running` Pods.
- A lifecycle test verifies both transient and final states.
- Shared operations are in `framework/` or `utils/`, while one-off constants stay local to the spec.
- Golden manifests compare semantic content and do not capture unstable metadata.
- OpenShift-specific behavior has an OCP test result when minikube cannot cover it.
- Full, focused, and indirect CI jobs receive new flags and dependencies where needed.
- The final response states which tests actually ran and which require a cluster.

## Verification commands

The skill recommends these checks before cluster-backed execution:

```bash
TEST_FILE=e2e-tests/tests/tier0/example_test.go
gofmt -w "$TEST_FILE"
go test ./e2e-tests/framework ./e2e-tests/utils
go test ./e2e-tests/tests/tier0 ./e2e-tests/tests/tier1 -run '^$'
go test . ./cmd/... ./internal/...
```

Do not treat an unqualified `go test ./...` as a unit-test command in this repository. It also runs the Ginkgo e2e suites under `e2e-tests/tests/tier0` and `e2e-tests/tests/tier1`. Some specs require configured clusters, binaries, or suite flags and can fail when that setup is unavailable.

Run a focused scenario with Ginkgo after preparing the required environment:

```bash
TEST_FILE=e2e-tests/tests/tier0/example_test.go
ginkgo run -v -r --focus-file="$TEST_FILE" e2e-tests/tests -- \
  --k8sdeploy-bin=k8sdeploy \
  --crane-bin="$PWD/crane" \
  --source-context=src \
  --target-context=tgt \
  --source-nonadmin-context=src-dev \
  --target-nonadmin-context=tgt-dev \
  --verbose-logs
```

Add scenario-specific flags such as `--plugin-dir`, `--cloud-storage`, or `--rclone-config-file` only when required.

## Maintaining the skill

Update `.claude/skills/crane-e2e-tests/SKILL.md` when the suite changes its directory layout, flags, framework abstractions, CI topology, or external dependencies.

Keep this document focused on team usage. Put detailed implementation rules in the skill itself so Claude Code receives them during execution.
