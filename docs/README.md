# Crane Documentation

Welcome to the Crane documentation. Crane is a Kubernetes migration tool that helps migrate workloads between clusters using a non-destructive pipeline: **export → transform → apply → validate**.

## Getting Started

- [Installation](./installation.md) — Prerequisites, building, and verifying the Crane binary
- [Quickstart Tutorial](./quickstart-tutorial.md) — End-to-end migration in under 10 minutes

## Command Reference

- [`crane export`](./commands/export.md) — Export namespace resources from a source cluster
- [`crane transform`](./commands/transform.md) — Transform exported resources using plugins and Kustomize
- [`crane apply`](./commands/apply.md) — Apply transformations and produce final manifests
- [`crane validate`](./commands/validate.md) — Validate final manifests against a target cluster
- [`crane transfer-pvc`](./commands/transfer-pvc.md) — Transfer PVC data between clusters via rsync

## Concepts

- [Multi-Stage Pipeline](./multistage-pipeline.md) — How Crane's multi-stage Kustomize transform pipeline works
- [State Migration](./state-migration.md) — How PVC data transfer works (rsync, endpoints, security)
- [Plugins](./plugins.md) — Built-in and custom plugin overview

## Tutorials

- [Stateless Migration](./stateless-migration-tutorial.md) — Migrate a stateless application between clusters
- [Stateful Migration](./stateful-migration-tutorial.md) — Migrate an application with persistent volumes
- [Troubleshooting](./troubleshooting.md) — Common issues and solutions

## Reference

- [Pre-Apply Validation Guide](./pre-apply-validation-guide.md) — Checklist for validating manifests before applying
- [Compatibility Matrix](./CRANE_COMPATIBILITY_MATRIX.md) — Supported resource types and migration boundaries

## Development

- [Development Guide](./development/README.md) — Architecture, setup, testing, and plugin development
- [Contributing](../CONTRIBUTING.md) — How to contribute to Crane
