---
name: crane-e2e-tests
description: Create, change, review, and debug Crane end-to-end tests under e2e-tests/, including Ginkgo scenarios, k8s-apps-deployer setup, plugin builds, fixtures, CI wiring, and result validation.
---

# Crane e2e tests

Use this workflow when working on `e2e-tests/` or its GitHub Actions setup.

## Read before editing

1. Read `e2e-tests/README.md` and the nearest tests in the target tier.
2. Read the relevant wrappers in `e2e-tests/framework/` before invoking a command directly.
3. Check `.github/actions/run-crane-e2e-tier/action.yml` and `.github/workflows/e2e-pr-tester.yaml` when a test needs a new binary, flag, cluster feature, or external repository.
4. Check whether the source API exists on minikube. Use a synthetic export fixture for an OpenShift-only source resource that minikube cannot create.
5. Check recent tests for the same resource lifecycle. Do not duplicate helpers or depend on an unexported function from another `_test.go` file.

## Runtime model

The suite uses Ginkgo v2 and Gomega. Tier packages have separate bootstrap files:

- `e2e-tests/tests/tier0`: release-gating migration paths.
- `e2e-tests/tests/tier1`: extended, plugin-specific, or environment-specific coverage.

CI performs these steps:

1. `.github/actions/setup-minikube-clusters` creates `src` and `tgt` profiles on one Docker network. It may also create `src-dev` and `tgt-dev` certificate users.
2. CI clones `migtools/k8s-apps-deployer`, installs it in a Python virtualenv, and invokes its `k8sdeploy` executable. Some discussions call this dependency `ocp-deployer`; use the repository and executable names present in the code.
3. CI installs the Ginkgo version from `go.mod` and builds Crane.
4. CI builds any test-specific plugins and passes their directory through `--plugin-dir`.
5. Ginkgo passes arguments after `--` to the suite's Go flags.

`K8sDeployApp` runs `k8sdeploy deploy`, `validate`, and `remove`. If the binary has a path, it prepends that directory to `PATH` so `ansible-playbook` from the same virtualenv remains available. Do not replace this wrapper with raw commands unless the test needs behavior the wrapper cannot express.

## Choose the test boundary

Use a live migration scenario when both clusters support the source and target APIs. Start with `NewMigrationScenario`, `NewScenarioPaths`, `PrepareSourceApp`, `RunCranePipelineWithChecks`, and `ApplyOutputToTarget`.

Use a synthetic export when the input API is unavailable on CI minikube. Write a realistic source manifest below `paths.ExportDir`, run `CraneRunner.Transform` and `CraneRunner.Apply`, inspect `output.yaml`, then apply the output to `tgt`. This still tests the plugin protocol, Crane stage orchestration, new-resource handling, rendering, and target API acceptance.

Do not install an OpenShift CRD merely to make an input object creatable. That would not test OpenShift API defaulting and adds unrelated cluster setup.

## Scenario rules

- Put the test in the lowest suitable tier and add the matching `Label("tier0")` or `Label("tier1")`.
- Add a behavior label such as `plugin`, `validate`, or `pvc-transfer` when useful.
- Keep feature-specific tests in the existing tier suites. Use labels for selection instead of adding another suite bootstrap or nested suite package.
- Use a unique namespace and temp directory prefix.
- Register `DeferCleanup` immediately after allocating resources. Cleanup must tolerate partial setup and test failure.
- Set `runner.WorkDir = paths.TempDir` so relative Crane artifacts stay isolated.
- Use `config.PluginDir` and `TransformOptions.PluginDir`; never assume a plugin exists in the user's default directory.
- Skip a plugin-specific test with a clear message when its explicit suite flag is absent. CI must pass the flag, so the test runs there.
- Assert behavior, not only command success. Inspect resource GVK, identity, transformed fields, removed source resources, warnings, and the target-cluster object.
- Prefer typed or unstructured YAML decoding over substring-only assertions.
- Use `Eventually` only for asynchronous cluster state. Do not add sleeps.
- Keep constants and helpers inside the spec when only that spec uses them. Move behavior to `framework/` when multiple tests need it; add unit tests for parsing or argument-building helpers.

## Lifecycle and polling

Treat creation, observation, and deletion as separate assertions.

- A final lifecycle assertion must verify that temporary Secrets, Pods, and other helper resources no longer exist. Use `Eventually` because Kubernetes deletion is asynchronous.
- Do not require several short-lived resources to exist in the same polling attempt when the command may delete them independently. A poll can observe one resource before cleanup and query another after cleanup. Retain an observed flag for each resource across attempts, or synchronize the observation before the command reaches garbage collection.
- Do not infer that a PVC is unused from the absence of a `Running` pod. Pending and initializing non-terminal Pods can already reference or mount the claim. Match the product rule against all applicable non-terminal Pods.
- Query object state rather than relying on command timing. If the behavior has both a transient state and a final state, assert both explicitly.

These rules come from human review of [#974](https://github.com/migtools/crane/pull/974), [#918](https://github.com/migtools/crane/pull/918), and [#927](https://github.com/migtools/crane/pull/927).

## Cross-platform coverage

Minikube CI does not cover OpenShift defaulting, SCC behavior, Routes, or downstream plugin packaging.

- For workload or PVC changes, use portable images and security contexts. Run the focused test on OpenShift when SCC admission can alter the result.
- Use `KubectlRunner.IsOpenShift()` only where platform behavior differs. Keep the main scenario and assertions shared.
- Record the OpenShift run in the PR test plan. A screenshot or job reference supplements, but does not replace, assertions in the test.
- Distinguish an upstream plugin installed through `plugin-manager`, a downstream bundled plugin, and a custom plugin build. Do not silently test a different plugin version than the change requires.
- When a product change adds a resource to pipeline output, update the golden manifests and every assertion that assumed the resource was whiteouted or absent.

Relevant review and follow-up changes are in [#1009](https://github.com/migtools/crane/pull/1009), [#1015](https://github.com/migtools/crane/pull/1015), [#991](https://github.com/migtools/crane/pull/991), [#992](https://github.com/migtools/crane/pull/992), [#941](https://github.com/migtools/crane/pull/941), and [#952](https://github.com/migtools/crane/pull/952).

## Test organization

- Do not couple test files through package-private helper definitions. Shared cluster operations belong in `framework/`; shared fixture and semantic comparison code belongs in `utils/`.
- Reuse verifier Pod creation, data reads, and cleanup when another scenario already implements the same operation.
- Keep exact expected CLI output near its test when it is used once. Extract constants only when several tests share the contract.
- For lifecycle scenarios, assert the final absence or retained state in addition to the successful command result.

Human reviewers requested these patterns in [#923](https://github.com/migtools/crane/pull/923), [#948](https://github.com/migtools/crane/pull/948), [#958](https://github.com/migtools/crane/pull/958), [#959](https://github.com/migtools/crane/pull/959), [#897](https://github.com/migtools/crane/pull/897), and [#918](https://github.com/migtools/crane/pull/918).

## DeploymentConfig conversion example

The reference test is `e2e-tests/tests/tier1/deploymentconfig_conversion_test.go`. Its input export is under `e2e-tests/testdata/deploymentconfig-conversion/export/`, and the captured proof is `e2e-tests/testdata/deploymentconfig-conversion/expected/output.yaml`.

PR `migtools/crane-plugin-openshift#34` adds the conversion. Conversion is enabled by default for compatible DeploymentConfigs. Set `convert-deploymentconfigs=false` only when the migration must preserve DeploymentConfigs. Until the PR merges, CI builds commit `ff642de7f58b9f28b255f2a17a1d01c5230148b2` through `.github/actions/build-openshift-plugin`.

The transform must receive:

```go
TransformOptions{
    ExportDir:    paths.ExportDir,
    TransformDir: paths.TransformDir,
    PluginDir:    config.PluginDir,
    OptionalFlags: `{"pvc-rename-map":"legacy-data:migrated-data"}`,
    Stages:       []string{"OpenShiftPlugin"},
}
```

Expected proof in `output/output.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: legacy-web
  namespace: deploymentconfig-conversion
spec:
  replicas: 0
  selector:
    matchLabels:
      app: legacy-web
  template:
    spec:
      volumes:
      - name: data
        persistentVolumeClaim:
          claimName: migrated-data
```

The complete assertion must also prove that no active `DeploymentConfig` remains, the ImageChange warning annotation exists, and SCC-injected IDs were removed. Finally, apply the rendered output and query the target Deployment.

## Local commands

Build the custom plugin:

```bash
git clone https://github.com/migtools/crane-plugin-openshift.git /tmp/crane-plugin-openshift
git -C /tmp/crane-plugin-openshift fetch --depth=1 origin ff642de7f58b9f28b255f2a17a1d01c5230148b2
git -C /tmp/crane-plugin-openshift checkout --detach FETCH_HEAD
mkdir -p /tmp/crane-e2e-plugins
go build -C /tmp/crane-plugin-openshift -o /tmp/crane-e2e-plugins/crane-plugin-openshift .
```

Run the focused test after the two clusters and `k8sdeploy` are available:

```bash
ginkgo run -v -r --focus-file=deploymentconfig_conversion_test.go e2e-tests/tests -- \
  --k8sdeploy-bin=k8sdeploy \
  --crane-bin="$PWD/crane" \
  --source-context=src \
  --target-context=tgt \
  --source-nonadmin-context=src-dev \
  --target-nonadmin-context=tgt-dev \
  --plugin-dir=/tmp/crane-e2e-plugins \
  --verbose-logs
```

## Verification

Run the cheapest checks first:

```bash
gofmt -w e2e-tests/tests/<tier>/<test>.go
go test ./e2e-tests/framework ./e2e-tests/utils
go test ./e2e-tests/tests/tier0 ./e2e-tests/tests/tier1 -run '^$'
go test . ./cmd/... ./internal/...
```

Do not use an unqualified `go test ./...` as a local unit-test command. It executes both Ginkgo e2e suites and requires their flags, clusters, and external binaries. Run the focused Ginkgo test with its required environment instead. Report separately which compile/unit checks passed and whether cluster-backed execution ran.

Before finishing, inspect the diff for these common failures:

- A new suite flag exists in only one tier bootstrap.
- The composite action path receives the dependency, but focused PR CI does not.
- Cleanup runs only after all setup succeeds.
- The test compares unstable server-generated metadata.
- A source-only OpenShift object is applied to minikube.
- The test checks output text but never validates or applies the rendered manifest.
- One `Eventually` call requires independently deleted resources to coexist.
- PVC-in-use logic considers only `Running` Pods.
- A lifecycle test observes creation but never verifies deletion.
- A tests-only PR does not say so in its title or summary.
- A dependency PR or plugin version is assumed but not pinned, merged, or documented in the test plan.
