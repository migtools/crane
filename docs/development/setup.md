# Development Setup

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| Go | 1.25+ (see `go.mod`) | Building and cluster-free testing |
| GNU Make, Bash 4.4+, Git | Available on the development host | Shared verification commands |
| kubectl | Latest stable | Deploying output to clusters; not required for `crane apply` (kustomize is embedded) |
| Docker | Latest stable | Container-based testing (optional) |
| kind or minikube | Latest stable | Local Kubernetes clusters for E2E tests |
| k8sdeploy | See the E2E guide | App deployment and validation during live E2E execution |

## Getting Started

### Clone the Repository

```bash
git clone https://github.com/konveyor/crane.git
cd crane
```

### Build

```bash
make build
```

### Install Locally

```bash
go install .
```

Or move the binary to your PATH:

```bash
sudo mv crane /usr/local/bin/
```

### Run Tests

```bash
# All cluster-free unit tests
make test-unit

# Specific package
go test ./internal/transform/...

# With verbose output
make test-unit GO_TEST_ARGS=-v

# With race detection
make test-unit GO_TEST_ARGS=-race

# Compile all E2E suites without running tests or benchmarks
make test-e2e-compile

# Unit coverage using the same settings as CI
make test-unit GO_TEST_ARGS=-v COVERAGE_PROFILE=coverage.out COVERAGE_MODE=atomic
```

The shared unit command excludes only the live `e2e-tests/tests/` subtree;
it includes E2E config, framework, and utility packages. An unqualified
`go test ./...` also executes live suites and is not a cluster-free check.

### Format Code

```bash
# Check tracked Go files without modifying them
make fmt-check

# Format the files changed by your task
gofmt -w path/to/changed.go
```

## Project Layout

Commands live in `cmd/<command>/` — each is a self-contained package with a cobra command constructor (`NewXxxCommand`), an `Options` struct holding flags, and `Complete/Validate/Run` methods.

Internal packages in `internal/` contain the business logic. Commands are thin wrappers that parse flags and call into internal packages.

## Setting Up Test Clusters

Live E2E execution needs source and target Kubernetes clusters, valid contexts,
`kubectl`, and `k8sdeploy`. See the [E2E guide](../../e2e-tests/README.md)
for suite flags and scenario-specific dependencies. The build and cluster-free
checks above do not require this setup.

### Using kind

```bash
# Create source cluster
kind create cluster --name source

# Create target cluster
kind create cluster --name target

# Verify contexts
kubectl config get-contexts
```

### Using minikube

```bash
# Create source cluster
minikube start -p source

# Create target cluster
minikube start -p target
```

## IDE Configuration

### VS Code

Recommended extensions:
- Go (golang.go)
- YAML (redhat.vscode-yaml)

### GoLand / IntelliJ

The project should be auto-detected as a Go module. Ensure the Go SDK is configured to match the version in `go.mod`.

## Common Development Tasks

### Adding a New Command

1. Create `cmd/<command>/<command>.go`
2. Define an `Options` struct with `Complete`, `Validate`, and `Run` methods
3. Create a `NewXxxCommand` function returning `*cobra.Command`
4. Register in `main.go`

### Adding a New Flag

Add the flag in the `addFlagsForOptions` function of the relevant command. Use `mapstructure` tags for viper integration.

### Working with Kubernetes API Objects

Use `*unstructured.Unstructured` for dynamic resources. Always check type assertions:

```go
obj, ok := item.(*unstructured.Unstructured)
if !ok {
    return fmt.Errorf("expected *unstructured.Unstructured but got %T", item)
}
```
