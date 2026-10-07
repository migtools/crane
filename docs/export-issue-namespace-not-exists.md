# Issue: Non-existent `--namespace` not reported on `crane export`

## Summary

`crane export` does not validate that `--namespace` / `-n` refers to an existing Namespace. A non-existent namespace is treated like an empty namespace, so the command can succeed while exporting little or nothing, which is misleading.

## Description

When exporting with an explicit namespace, users expect Crane to fail immediately if that namespace does not exist on the cluster (similar to `kubectl get namespace <name>`). Instead, the export appears to proceed: namespaced discovery yields no resources, and cluster-scoped steps may still run, producing logs/output that suggest a normal run rather than a configuration error.

This is risky for migration workflows because a typo can produce an incomplete export without an obvious failure.

## Steps to reproduce

1. Point `kubectl` (or Crane) at a cluster where namespace `whocares` does **not** exist (e.g. minikube or any test cluster).
2. Run:

   ```bash
   crane export --namespace whocares --context "$CTX"
   ```

   Adjust `--context` / kubeconfig as needed.

3. Observe exit code and logs.

## Actual behavior

- The command does not report that the namespace is missing.
- Export continues; output may show empty or minimal namespaced results and cluster-scoped discovery/filtering messages (e.g. no matching cluster-scoped resources, empty `_cluster/` directory), without a clear error that the namespace is invalid.

## Expected behavior

- If the user specifies a namespace that does not exist, Crane should **fail with a clear error** (e.g. `namespaces "whocares" not found` or equivalent) and a **non-zero exit code**, before or at the start of export, so users cannot mistake a wrong `-n` for a successful empty export.

## Environment (optional, for GitHub)

- Crane version: _fill in_
- Kubernetes version: _fill in_
- OS: _fill in_

## Notes for filing on GitHub

Copy the sections above (omit or fill **Environment**) into a new issue. Treating this as a bug or correctness/UX defect is reasonable: silent “success” with the wrong scope is easy to misread as “nothing to export” rather than “wrong target.”
