---
name: crane-e2e-test-authoring
description: Use when creating, updating, debugging, or reviewing Crane e2e tests, including MTA or Polarion cases, Kubernetes workloads, migrations, RBAC, PVCs, plugins, validation, and export-transform-apply flows.
---

# Crane E2E Test Authoring

Use this skill for Crane end-to-end test work. Keep the workflow generic and
adapt it to the specific testcase instead of assuming a PVC or StorageClass
scenario.

## Approval Gate

Always begin in read-only discovery mode.

1. Read the testcase, issue, CSV entry, or acceptance criteria.
2. Inspect related tests, framework helpers, fixtures, and application deployer
   definitions.
3. Report what the testcase is intended to verify, the proposed flow, reusable
   helpers, files that would change, assumptions, and open questions.
4. Stop and wait for explicit user approval before editing test files.

Statements such as "look into this", "analyze this", or "tell me what you
understand" are not approval to edit. Approval must be explicit, such as
"implement it", "start writing the test", or "proceed".

## Discovery

Before implementation:

- Read the Polarion testcase when one is provided.
- Read the matching feature-plan CSV entry when present.
- Search `e2e-tests/tests` for the closest application and behavior.
- Inspect `e2e-tests/framework` before adding helpers.
- Inspect `k8s-apps-deployer` roles, templates, seed data, and validation tasks
  when the test deploys a supported application.
- Identify source and target clusters, namespaces, contexts, identities,
  application data, expected manifests, and cleanup requirements.
- Check whether the testcase is already covered by a related test and preserve
  distinct acceptance criteria.

## Test Design

Organize the test into explicit Ginkgo steps:

1. Set up contexts, namespaces, RBAC, and temporary paths.
2. Deploy and validate the source application.
3. Seed or capture source data and fingerprints.
4. Quiesce workloads when storage consistency requires it.
5. Run the Crane operation under test.
6. Assert intermediate artifacts directly, especially rendered manifests or
   reports.
7. Apply or deploy the target workload.
8. Validate target readiness, resource properties, and data integrity.
9. Assert helper-resource cleanup.

Use precise failure messages for assertions. Prefer exact field or line
matching over substring checks when names can be prefixes of one another.

## Contexts And Permissions

- Use `NewMigrationScenario` for application and runner construction.
- Use `SrcAppNonAdmin`, `TgtAppNonAdmin`, and `CraneNonAdmin` when the behavior
  must work as a non-admin user.
- Use `SetupActiveKubectlRunners` for namespace-scoped non-admin RBAC tests.
- Use admin runners only for cluster-scoped setup and teardown, such as
  StorageClass operations, node discovery, namespace lifecycle, and RBAC
  grants.
- Do not confuse admin setup permissions with the identity executing the
  behavior under test.
- Use unique namespaces such as `mta-<id>-<purpose>`.

## Reuse And Helpers

- Reuse existing framework helpers before writing new code.
- Put genuinely reusable behavior in `e2e-tests/framework` or `e2e-tests/utils`.
- Add a doc comment to new exported helpers.
- Prefer pure error-returning helpers for shared validation instead of coupling
  framework packages to Ginkgo assertions.
- Do not rely on a helper defined in another test file merely because tests
  share the same Go package.
- Keep scenario-specific helpers local to the test.
- Do not refactor separate Polarion scenarios into a large parameterized helper
  unless the shared behavior is stable and the refactor is explicitly useful.

## Application Validation

- Follow the application deployer's actual database, collection, file paths,
  credentials, labels, and seed data.
- Validate source data before migration.
- Validate target data using application-specific fingerprints, checksums,
  row/document counts, or file content as appropriate.
- Do not rely only on a generic deployer validation command when the testcase
  requires a stronger or more specific assertion.

## Storage And Migration Patterns

For storage-related tests:

- Resolve the source StorageClass using an admin context when required.
- Reuse `PrepareDestinationStorageClass` or the established target-cluster
  fallback pattern.
- Prefer an existing different StorageClass when available.
- Clone a compatible source/default class only when no alternative exists.
- Register cleanup for a class created by the test.
- Verify destination PVC names, `Bound` status, StorageClass, data, and helper
  resources.
- Use `AssertNoTransferPVCLeftovers` after each transfer where applicable.

## Local Execution

- Use an absolute `--crane-bin` path when running tests locally. Crane commands
  execute from scenario temporary directories, so a relative `./crane` path may
  not resolve unless the runner explicitly supports it.
- Use an absolute `--k8sdeploy-bin` path when the scenario uses the application
  deployer. Verify that every required executable exists before starting a live
  run.
- Prefer direct Ginkgo execution for live cluster tests because it streams
  `By` steps, command output, and cleanup progress:
  `ginkgo run -r --timeout=90m -v --focus='MTA-XXX' e2e-tests/tests/ -- ...`.
- Pass suite-specific flags after Ginkgo's `--` separator, including binary
  paths, cluster contexts, non-admin contexts, and logging options.
- Do not use `--run-as=admin` when validating non-admin behavior. Use admin
  runners only for setup, teardown, RBAC grants, cluster-scoped discovery, and
  other operations that require elevated permissions.
- Build a temporary binary from the intended worktree, not the repository root
  by accident. Verify the selected revision and binary path before running the
  test.
- Use `go test -run '^$'` for compile-only checks; use Ginkgo for live cluster
  execution. Avoid diagnosing a live E2E run from buffered `go test` output.
- Do not require the application deployer for scenarios that can create and
  validate their Kubernetes resources directly with framework runners.
- Match CI cluster prerequisites before diagnosing product behavior. For
  Minikube direct PVC transfer, verify the ingress addon, SSL passthrough,
  HTTPS host port 443, stable node IPs, and required source/target networking.
- Do not modify cluster configuration, framework code, or external systems
  unless the user explicitly requests it.

## Verification

After explicit approval and implementation:

- Run `gofmt` on changed Go files.
- Compile the affected package.
- Run focused Ginkgo tests with the configured contexts.
- Run relevant framework/unit tests.
- Expect recursive focused runs to report multiple suite summaries, with
  unrelated specs skipped; confirm the intended spec passed.
- Allow for environment-dependent waits during PVC binding, ingress or
  endpoint readiness, transfer startup, and asynchronous cleanup. Use verbose
  Ginkgo output to distinguish progress from a hang.
- Review `git diff`, `git diff --check`, and `git status`.
- Report passed checks, blocked checks, and environment prerequisites
  separately.

## Cleanup Requirements

- Register cleanup for every namespace, temporary directory, helper resource,
  and test-created cluster object.
- Remove blocking metadata, such as test finalizers, before deleting the
  namespace that contains the resource.
- Verify that test namespaces and transfer helper resources are gone after a
  live run, including when the test fails partway through.

## External Tracking

When explicitly asked to track a testcase:

- Inspect the parent GitHub issue and existing sub-issues first.
- Follow the repository's existing title, label, assignee, and body style.
- Create or link the issue only after the user explicitly requests the write
  operation.
