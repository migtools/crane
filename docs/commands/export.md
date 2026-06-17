# crane export

Export namespace resources from a source Kubernetes cluster to disk.

## Synopsis

```bash
crane export [flags]
```

## Description

`crane export` discovers all API types in a Kubernetes cluster, lists objects in the specified namespace (plus related cluster-scoped RBAC resources), and writes manifests to an export directory. This is the first step in the Crane migration pipeline.

Exported resources are written as individual YAML files under `export/resources/<namespace>/`. Cluster-scoped resources related to the namespace (ClusterRoleBindings, ClusterRoles, SCCs) are written to `export/resources/<namespace>/_cluster/`. Any errors encountered during listing are recorded in `export/failures/<namespace>/`.

### CRD Collection

When custom resources are found in the namespace, Crane automatically collects their corresponding CustomResourceDefinitions. Operator-managed CRDs (identified via owner references) are skipped, since they should be installed by the operator on the target cluster rather than migrated directly. If the migration user lacks permission to read CRDs, Crane logs a warning and continues — the assumption is that the CRDs already exist on the target.

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--export-dir` | `-e` | `export` | The path where files are exported |
| `--label-selector` | `-l` | | Restrict export to resources matching a label selector |
| `--namespace` | `-n` | _(context default)_ | Namespace to export |
| `--crd-skip-group` | | | API groups to skip for CRD export (repeatable) |
| `--crd-include-group` | | | API groups to force-include for CRD export (repeatable) |
| `--as-extras` | | | Extra impersonation info (format: `key=val1,val2;key2=val3`) |
| `--qps` | `-q` | `100` | Query-per-second rate for API requests |
| `--burst` | `-b` | `1000` | API burst rate |
| `--overwrite` | | `false` | Overwrite the export directory if it already exists |

Standard kubeconfig flags (`--kubeconfig`, `--context`, `--cluster`, `--as`, `--as-group`, etc.) are also available.

> **Note:** `--context` is mutually exclusive with `--cluster`, `--server`, `--user`, and `--token`.

## Output Structure

```text
export/
├── resources/
│   └── <namespace>/
│       ├── Deployment_apps_v1_<ns>_<name>.yaml
│       ├── Service__v1_<ns>_<name>.yaml
│       ├── ConfigMap__v1_<ns>_<name>.yaml
│       └── _cluster/
│           ├── ClusterRoleBinding_rbac.authorization.k8s.io_v1_clusterscoped_<name>.yaml
│           ├── ClusterRole_rbac.authorization.k8s.io_v1_clusterscoped_<name>.yaml
│           └── CustomResourceDefinition_apiextensions.k8s.io_v1_clusterscoped_<name>.yaml
└── failures/
    └── <namespace>/
        └── <error-files>
```

Resource filenames follow the format: `Kind_group_version_namespace_name.yaml`

### Admin vs Non-Admin Migration

Crane supports migration for both cluster-admin and non-admin users:

**Admin users** have full access and can export all resource types including cluster-scoped resources (ClusterRoles, ClusterRoleBindings, CRDs, etc.).

**Non-admin users** can still use Crane with reduced permissions:
- Export gracefully handles `Forbidden` errors — resources the user cannot list are skipped with a warning, and export continues with accessible resources
- If the user cannot verify the namespace exists (no `get namespaces` permission), Crane logs a warning and proceeds
- CRD collection skips CRDs the user cannot read, with a warning that they should already exist on the target cluster
- Export only exits with a non-zero code if **all** resource types return Forbidden, indicating the user has no list permissions at all

For the full non-admin pipeline, pair export with `crane apply --skip-cluster-scoped` (see [crane apply](./apply.md)).

## Examples

### Basic namespace export

```bash
crane export -n my-app
```

### Export with label selector

```bash
crane export -n my-app --label-selector "app=frontend"
```

### Export with custom directory

```bash
crane export -n my-app --export-dir ./migration/export
```

### Export with impersonation

```bash
crane export -n my-app --as system:serviceaccount:my-app:deployer
```

### Export with extra impersonation info

```bash
crane export -n my-app \
  --as user@example.com \
  --as-extras "scope=read,write;project=my-project"
```

### Export skipping specific CRD groups

```bash
crane export -n my-app --crd-skip-group monitoring.coreos.com
```

## Common Errors

| Error | Cause | Solution |
|-------|-------|----------|
| `namespace must be set` | No namespace specified and none in kubeconfig context | Use `-n <namespace>` |
| `namespaces "X" not found` | Namespace does not exist | Verify namespace name |
| `cannot verify namespace exists` | Insufficient RBAC (warning only) | Export proceeds; verify namespace exists manually |
| `extras requires specifying a user or group` | `--as-extras` used without `--as` | Add `--as` or `--as-group` flag |
| `export directory "X" already exists` | Export directory from a previous run | Use `--overwrite` to replace it |
| Non-zero exit with aggregated error | All namespace list calls returned Forbidden | Ensure service account has list permissions on at least one namespace |

## Next Steps

After exporting, transform the resources:

```bash
crane transform --export-dir export
```

See [crane transform](./transform.md) for details.
