# Crane Development Guide

This directory contains detailed guides for developers contributing to Crane.

## Guides

- [Architecture](./architecture.md) — Pipeline design, package structure, and data flow
- [Development Setup](./setup.md) — Setting up your development environment
- [Testing](./testing.md) — Unit tests, table-driven patterns, and E2E testing
- [Plugin Development](./plugin-development.md) — Writing custom transformation plugins
- [Crane E2E agent skill](./crane-e2e-skill.md) - Using repository-specific E2E guidance with Claude Code
- [Vertex WIF smoke test](./vertex-wif-smoke-test.md) — Validating keyless Claude access from GitHub Actions

## Quick Reference

```bash
# Build all packages and write ./crane
make build

# Cluster-free unit tests, including E2E helpers
make test-unit

# Compile every E2E suite without executing tests
make test-e2e-compile

# Check tracked Go formatting without rewriting files
make fmt-check
```

`go test ./...` also runs cluster-backed suites under `e2e-tests/tests/`.
Use the shared commands above for cluster-free checks. CI uses the same build,
unit-test, and E2E compilation targets. The format check is a local command.

Shared contributor rules are in [AGENTS.md](../../AGENTS.md). Claude Code
loads those rules through [CLAUDE.md](../../CLAUDE.md).

## Project Structure

```text
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
│   ├── kustomize/          # Embedded kustomize runner (krusty API)
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
- [migtools/pvc-transfer](https://github.com/migtools/pvc-transfer) — PV migration library
