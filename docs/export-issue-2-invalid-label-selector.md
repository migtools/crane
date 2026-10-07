# Issue #2: Invalid `--label-selector` on `crane export`

## Summary

Invalid `--label-selector` on `crane export` triggers a BadRequest on every resource list, floods logs and `failures/`, yet the process can still exit **0**.

## Description

`crane export` forwards `--label-selector` to the API without validating selector syntax. The apiserver returns **400 BadRequest** for each `List`. Each failure is logged and written under `export-dir/failures/<namespace>/`, but `Run()` only returns an error for **write** failures, not list failures—so the command may succeed from the shell’s point of view even when every list failed for the same bad selector.

## Steps to reproduce

1. Use any valid kube context and namespace that exists.
2. Run export with an invalid label selector, for example:

   ```bash
   crane export -e /tmp/crane-export-test --label-selector 'key in (unclosed'
   ```

   Any string that is not valid Kubernetes label selector grammar will do.

## Actual behavior

- Many error log lines (one per API type that gets listed).
- Many files under `failures/<namespace>/` (e.g. `pods.yaml`, `configmaps.yaml`, …) each reflecting a BadRequest-style error.
- Command often exits with code **0** if writing those failure files and directories succeeds.

## Expected behavior

- Fail **fast** with a **single** clear error that the label selector is invalid.
- **Non-zero** exit code.
- No broad listing pass and no large set of failure artifacts for this user error.

## Resolution (in tree)

Validate `--label-selector` with `k8s.io/apimachinery/pkg/labels.Parse` in `ExportOptions.Validate()` (`cmd/export/export.go`) before discovery/listing. Tests: `TestValidate` in `cmd/export/export_test.go`.
