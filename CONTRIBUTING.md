# Contributing to Crane

Thank you for your interest in contributing to Crane! This guide will help you get started.

## Quick Start

1. Fork and clone the repository:
   ```bash
   git clone https://github.com/<your-username>/crane.git
   cd crane
   ```

2. Run the cluster-free checks from the repository root:
    ```bash
    make build
    make test-unit
    make test-e2e-compile
    make fmt-check
    ```

3. Run focused live E2E tests when their cluster and tool prerequisites are
   available. See the [E2E guide](e2e-tests/README.md).

4. Submit a pull request

## Prerequisites

- **Go** 1.25+ (see `go.mod` for exact version)
- **GNU Make, Bash 4.4+, and Git** for the shared verification commands
- **kubectl** (for deploying output to clusters and for `crane validate` with live cluster mode)
- **Docker** (optional, for container-based testing)
- **k8sdeploy** and source/target Kubernetes contexts for live E2E testing

The build, unit tests, E2E compilation, and format check do not require a
kubeconfig, running clusters, or `k8sdeploy`. Go may download module dependencies.

## Verification commands

| Command | Purpose |
| --- | --- |
| `make build` | Build all packages and write the `./crane` binary |
| `make test-unit` | Test all packages except the live `e2e-tests/tests/` subtree, including E2E config, framework, and utility packages |
| `make test-e2e-compile` | Compile every E2E suite without running tests or benchmarks |
| `make fmt-check` | Check tracked Go files without rewriting them |

An unqualified `go test ./...` also runs the live Ginkgo suites. Use
`make test-unit` for cluster-free checks. Run a specific unit package directly
when useful, for example `go test ./internal/transform/...`.

The targets accept `GO` to select the Go executable and `GO_TEST_ARGS` for
test flags. For the same unit coverage settings as CI, use:

```bash
make test-unit GO_TEST_ARGS=-v COVERAGE_PROFILE=coverage.out COVERAGE_MODE=atomic
go tool cover -func=coverage.out
```

`make fmt-check` can report pre-existing formatting differences. Format the
files changed by your task and report unrelated differences separately.

## Development Setup

For detailed guides on architecture, testing, and advanced development workflows, see the [Development Documentation](docs/development/README.md).

## Code Standards

### Go Conventions

- Follow standard Go idioms and [Effective Go](https://go.dev/doc/effective_go) practices
- Use `gofmt` for formatting
- Prefer explicit error messages with context — include the actual type received, the API resource being processed, and enough context to debug without re-running

### Error Handling

Error messages must be actionable:

```go
// Good — shows actual object type
fmt.Errorf("expected *unstructured.Unstructured but got %T", object)

// Bad — shows nil pointer
fmt.Errorf("expected *unstructured.Unstructured but got %T", u)
```

### Working with Kubernetes API Objects

- Use `*unstructured.Unstructured` for dynamic resources
- Always check type assertions and provide informative error messages
- Include the actual resource type and API resource name in errors

## Commit and PR title conventions

Commit messages use Conventional Commits, such as `fix: handle empty namespace`
or `docs: clarify cluster-free checks`.

PR titles use the aliases accepted by the
[Konveyor versioning documentation](https://github.com/konveyor/release-tools/blob/main/VERSIONING.md):

- `:bug:` for bug fixes, for example `:bug: Fix empty namespace handling`
- `:book:` for documentation
- `:seedling:` for infrastructure and other changes
- `:test_tube:` for integration and E2E tests

The validator also accepts `:warning:`, `:sparkles:`, and `:ghost:`. A
Conventional Commit prefix alone does not satisfy the PR title check.
Include issue references where applicable.

## Pull Request Guidelines

**Title:** Clear and concise (under 70 characters)

**Body must include:**
- **Summary** — what changed and why
- **Impact** — severity, affected components
- **Test plan** - exact commands run and whether focused live E2E execution ran

**Before submitting:**

- [ ] Code compiles: `make build`
- [ ] Cluster-free unit tests pass: `make test-unit`
- [ ] E2E packages compile: `make test-e2e-compile`
- [ ] Formatting checked: `make fmt-check`, with unrelated existing differences reported
- [ ] Error messages are informative
- [ ] No unnecessary refactoring in the same PR
- [ ] Backward compatible (or documented breaking change)

## Testing

- All new features require tests
- Bug fixes should include regression tests when possible
- Use table-driven tests for multiple scenarios
- Test files: `*_test.go` in the same package
- E2E tests: `e2e-tests/`; compile with `make test-e2e-compile` and execute with the required cluster setup

See [Testing Guide](docs/development/testing.md) for detailed testing documentation.

## Reporting Issues

- Check existing issues before filing a new one
- Include reproduction steps, expected behavior, and actual behavior
- Include Crane version, Go version, and Kubernetes version

## Code of Conduct

Be respectful and constructive. We follow the [Konveyor community guidelines](https://www.konveyor.io/).
