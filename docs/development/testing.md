# Testing Guide

## Unit Tests

### Running Tests

```bash
# All tests
go test ./...

# Specific package
go test ./internal/transform/...

# Verbose output
go test -v ./internal/validate/...

# With coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

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

```
e2e-tests/
├── framework/              # Test framework (app deployment, kubectl wrappers, pipeline)
├── tests/                  # Test cases (mtc_XXX_*.go)
├── golden-manifests/       # Expected output fixtures
└── utils/                  # Helper utilities
```

### Running E2E Tests

E2E tests require running Kubernetes clusters:

```bash
cd e2e-tests
go test -v ./tests/...
```

### Golden Manifests

The `golden-manifests/` directory contains expected export and output for known applications (e.g., `redis`, `simple-nginx-nopv`). E2E tests compare actual output against these fixtures.

### Writing E2E Tests

Follow existing patterns in `e2e-tests/tests/`:

1. Use `framework.DeployApp()` to set up the test application
2. Run the Crane pipeline using `framework.RunPipeline()`
3. Compare output against golden manifests or verify specific resource properties

## CI

- **Unit tests and build**: Run on every push via `.github/workflows/go.yml`
- **E2E tests**: Run on push to main and on a nightly cron schedule via `.github/workflows/run-e2e-tests.yml`

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
