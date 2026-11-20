# Testing Guide

## Cluster-free checks

### Running Tests

```bash
# Build all packages and write ./crane
make build

# All cluster-free unit tests, including E2E helpers
make test-unit

# Compile every E2E suite without running tests or benchmarks
make test-e2e-compile

# Check tracked Go formatting without rewriting files
make fmt-check

# Specific package
go test ./internal/transform/...

# Verbose output
go test -v ./internal/validate/...

# Unit coverage using the same settings as CI
make test-unit GO_TEST_ARGS=-v COVERAGE_PROFILE=coverage.out COVERAGE_MODE=atomic
go tool cover -html=coverage.out
```

`make test-unit` derives its package list from `go list ./...` and excludes
the live `e2e-tests/tests/` subtree. It includes `e2e-tests/config`,
`e2e-tests/framework`, `e2e-tests/utils`, and verification-tool tests in `hack/`.
A bare `go test ./...` also executes the live Ginkgo suites and can require
clusters and external tools.

Use `GO_TEST_ARGS` for flags such as `-v`, `-race`, or `-count=1`. Set `GO`
to select another Go executable. Set `COVERAGE_PROFILE` to enable coverage;
`COVERAGE_MODE` defaults to `atomic`.

The format check can report existing differences outside the changed files.
Report those separately and format only files belonging to the task.

### Test Conventions

- Test files are `*_test.go` in the same package
- Use table-driven tests for multiple scenarios
- Test helpers go in `test_helpers.go` files (e.g., `internal/transform/test_helpers.go`)

### Table-Driven Test Example

```go
func TestValidateStageName(t *testing.T) {
    tests := []struct {
        name      string
        stageName string
        wantErr   bool
    }{
        {"valid plugin stage", "10_KubernetesPlugin", false},
        {"valid custom stage", "30_CustomEdits", false},
        {"missing number", "KubernetesPlugin", true},
        {"missing name", "10_", true},
        {"empty", "", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidateStageName(tt.stageName)
            if (err != nil) != tt.wantErr {
                t.Errorf("ValidateStageName(%q) error = %v, wantErr %v",
                    tt.stageName, err, tt.wantErr)
            }
        })
    }
}
```

### Testing with Temporary Directories

Many tests create temporary directories for export/transform/output:

```go
func TestSomething(t *testing.T) {
    tmpDir := t.TempDir() // Automatically cleaned up
    exportDir := filepath.Join(tmpDir, "export")
    os.MkdirAll(exportDir, 0700)
    // ... test logic
}
```

## E2E Tests

### Location

E2E tests live in `e2e-tests/`:

```text
e2e-tests/
├── framework/              # Test framework (app deployment, kubectl wrappers, pipeline)
├── tests/                  # Test cases (mtc_XXX_*.go)
├── golden-manifests/       # Expected output fixtures
└── utils/                  # Helper utilities
```

### Compiling E2E suites

From the repository root:

```bash
make test-e2e-compile
```

This compiles all packages below `e2e-tests/tests/` with test and benchmark
execution disabled. It does not validate cluster behavior.

### Running E2E tests

E2E tests require running Kubernetes clusters:

```bash
ginkgo run -v --recurse e2e-tests/tests -- \
  --k8sdeploy-bin=/path/to/k8sdeploy \
  --crane-bin="$PWD/crane" \
  --source-context=src \
  --target-context=tgt
```

See the [E2E guide](../../e2e-tests/README.md) for non-admin contexts,
plugin flags, indirect transfer settings, and focused execution. The
[Crane E2E skill](crane-e2e-skill.md) documents the agent workflow for these tests.

### Golden Manifests

The `golden-manifests/` directory contains expected export and output for known applications (e.g., `redis`, `simple-nginx-nopv`). E2E tests compare actual output against these fixtures.

### Writing E2E Tests

Follow existing patterns in `e2e-tests/tests/`:

1. Use `framework.DeployApp()` to set up the test application
2. Run the Crane pipeline using `framework.RunPipeline()`
3. Compare output against golden manifests or verify specific resource properties

## CI

- Build, unit tests with coverage, and E2E suite compilation use `make build`,
  `make test-unit`, and `make test-e2e-compile` in `.github/workflows/go.yml`.
- Live direct and indirect PR tests use `e2e-pr-tester.yaml` and
  `e2e-indirect-pr-tester.yaml`. Main-branch and nightly suites use
  `run-e2e-tier0-tests.yml` and `run-e2e-tier1-tests.yml`.
- `make fmt-check` is available locally. It is not a required CI step while
  the repository has existing formatting differences.

## Test Coverage

To check coverage for a specific package:

```bash
go test -coverprofile=cover.out ./internal/transform/
go tool cover -func=cover.out
```

Guidelines:
- All new features require tests
- Bug fixes should include regression tests
- Focus on testing behavior, not implementation details
