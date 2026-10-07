# Issue #319: Support for Validating Against Metadata for Any Cluster

**GitHub:** https://github.com/migtools/crane/issues/319
**Status:** Open

---

## Summary

Extend `crane validate` to support offline validation by accepting a file containing the target cluster's API metadata, removing the requirement for a live cluster connection.

## Problem

`crane validate` currently requires a live kubeconfig connection to the target cluster. This doesn't work for air-gapped environments, CI/CD pipelines without cluster access, ArgoCD/GitOps workflows where only ArgoCD holds credentials, or pre-provisioning checks against clusters that don't exist yet.

## Requirements

| # | Requirement |
|---|-------------|
| R1 | Accept a cluster metadata file describing the target cluster's API surface (served groupVersions, kinds, and resource names) |
| R2 | Perform the same GVK compatibility checks and produce the same reports/failures as live validation |
| R3 | Provide a way to generate the metadata file from a live cluster |
| R4 | Provide a way to generate the metadata file from standard `kubectl` output, for environments where crane is not installed on the target side |
| R5 | Clearly indicate in the validation report whether live or offline mode was used, and the metadata source |
| R6 | Warn when the metadata file is stale |

## Key Use Cases

- **Air-gapped migration** -- target cluster on a restricted network with no direct access
- **ArgoCD / GitOps** -- only ArgoCD has cluster credentials; metadata is checked into the GitOps repo and validated in CI before sync
- **CI/CD pipelines** -- validate manifests in a pipeline that has no network path to the target cluster
- **Pre-provisioning** -- validate against a planned target cluster profile before it is provisioned
