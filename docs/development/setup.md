# Development Setup

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| Go | 1.21+ (see `go.mod`) | Building and testing |
| kubectl | Latest stable | Required by `crane apply` and `crane validate` |
| Docker | Latest stable | Container-based testing (optional) |
| kind or minikube | Latest stable | Local Kubernetes clusters for E2E tests |

## Getting Started

### Clone the Repository

```bash
git clone https://github.com/konveyor/crane.git
cd crane
```

### Build

```bash
go build -o crane main.go
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
# All unit tests
go test ./...

# Specific package
go test ./internal/transform/...

# With verbose output
go test -v ./cmd/export/...

# With race detection
go test -race ./...
```

### Format Code

```bash
gofmt -w .
```

## Project Layout

Commands live in `cmd/<command>/` — each is a self-contained package with a cobra command constructor (`NewXxxCommand`), an `Options` struct holding flags, and `Complete/Validate/Run` methods.

Internal packages in `internal/` contain the business logic. Commands are thin wrappers that parse flags and call into internal packages.

## Setting Up Test Clusters

For E2E testing, you need source and target Kubernetes clusters:

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
