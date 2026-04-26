# Contributing to Crane

Thank you for your interest in contributing to Crane! This guide will help you get started.

## Quick Start

1. Fork and clone the repository:
   ```bash
   git clone https://github.com/<your-username>/crane.git
   cd crane
   ```

2. Build the binary:
   ```bash
   go build -o crane main.go
   ```

3. Run tests:
   ```bash
   go test ./...
   ```

4. Submit a pull request

## Prerequisites

- **Go** 1.21+ (see `go.mod` for exact version)
- **kubectl** (for running Crane commands that interact with clusters)
- **Docker** (optional, for container-based testing)
- Access to a Kubernetes cluster (for E2E testing)

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

## Commit Messages

Use conventional commits format:

- `fix:` for bug fixes
- `feat:` for new features
- `refactor:` for code restructuring
- `test:` for test additions
- `docs:` for documentation

Include issue references where applicable: `fix: improve error messages (#197)`

## Pull Request Guidelines

**Title:** Clear and concise (under 70 characters)

**Body must include:**
- **Summary** — what changed and why
- **Impact** — severity, affected components
- **Test plan** — how to verify

**Before submitting:**

- [ ] Code compiles: `go build ./...`
- [ ] Tests pass: `go test ./...`
- [ ] Error messages are informative
- [ ] No unnecessary refactoring in the same PR
- [ ] Backward compatible (or documented breaking change)

## Testing

- All new features require tests
- Bug fixes should include regression tests when possible
- Use table-driven tests for multiple scenarios
- Test files: `*_test.go` in the same package
- E2E tests: `e2e-tests/`

See [Testing Guide](docs/development/testing.md) for detailed testing documentation.

## Reporting Issues

- Check existing issues before filing a new one
- Include reproduction steps, expected behavior, and actual behavior
- Include Crane version, Go version, and Kubernetes version

## Code of Conduct

Be respectful and constructive. We follow the [Konveyor community guidelines](https://www.konveyor.io/).
