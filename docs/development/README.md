# Crane Development Guide

This directory contains detailed guides for developers contributing to Crane.

## Guides

- [Architecture](./architecture.md) — Pipeline design, package structure, and data flow
- [Development Setup](./setup.md) — Setting up your development environment
- [Testing](./testing.md) — Unit tests, table-driven patterns, and E2E testing
- [Plugin Development](./plugin-development.md) — Writing custom transformation plugins

## Quick Reference

```bash
# Build
go build -o crane main.go

# Test
go test ./...

# Lint
gofmt -l .
```

## Project Structure

```
crane/
├── cmd/                    # CLI command implementations
│   ├── apply/              # crane apply
│   ├── export/             # crane export
│   ├── transform/          # crane transform
│   ├── validate/           # crane validate
│   ├── transfer-pvc/       # crane transfer-pvc
│   └── ...
├── internal/               # Internal packages
│   ├── apply/              # Kustomize apply logic
│   ├── transform/          # Orchestrator, stages, writer
│   ├── validate/           # Scanner, matcher, report
│   ├── file/               # File helper utilities
│   ├── flags/              # Global CLI flags
│   └── plugin/             # Plugin loading and execution
├── e2e-tests/              # End-to-end tests
│   ├── framework/          # Test framework utilities
│   ├── tests/              # Test cases
│   ├── golden-manifests/   # Expected output fixtures
│   └── utils/              # Test helper utilities
└── docs/                   # Documentation
```

## Related Repositories

- [konveyor/crane-lib](https://github.com/konveyor/crane-lib) — Transformation logic library
- [konveyor/crane-plugins](https://github.com/konveyor/crane-plugins) — Community plugins
- [konveyor/crane-plugin-openshift](https://github.com/konveyor/crane-plugin-openshift) — OpenShift plugin
- [backube/pvc-transfer](https://github.com/backube/pvc-transfer) — PV migration library
