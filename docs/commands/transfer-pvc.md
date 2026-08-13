# crane transfer-pvc

Transfer PersistentVolumeClaim resources and volume data between clusters.

## Synopsis

```bash
crane transfer-pvc [flags]
```

## Description

The `transfer-pvc` subcommand transfers a PersistentVolumeClaim resource and its volume data to a destination cluster. It supports two primary transfer modes:

1. **Direct Mode**: Establishes a direct connection between source and destination clusters by creating a public endpoint (e.g., `route` or `ingress`) in the destination namespace. An rsync client Pod in the source transfers data directly to an rsync daemon Pod in the destination.
2. **Indirect Mode**: Enables transfers between clusters without direct network connectivity. Data is uploaded to an S3-compatible cloud storage bucket by a source Pod and then downloaded to the destination PVC by a destination Pod.

`transfer-pvc` supports transfers between different clusters or within the same cluster. When performing transfers within the same cluster and namespace, the source and destination PVC names must be different.

## Example

### Direct Transfer
```bash
crane transfer-pvc --source-context=<source> --destination-context=<destination> --pvc-name=<pvc_name> --endpoint=route
```

### Indirect Transfer
```bash
crane transfer-pvc --source-context=source --destination-context=destination \
  --pvc-name=data-pvc \
  --cloud-storage=s3://my-bucket/transfer-path \
  --rclone-config-secret=rclone-secret
```

## Flags

| Flag | Type | Required | Description |
|------|------|----------|-------------|
| `--source-context` | string | Yes | Kube context of the source cluster |
| `--destination-context` | string | Yes | Kube context of the destination cluster |
| `--pvc-name` | string | Yes | Mapping of source/destination PVC names (see [PVC Options](#pvc-options)) |
| `--pvc-namespace` | string | No | Mapping of source/destination PVC namespaces (see [PVC Options](#pvc-options)) |
| `--dest-storage-class` | string | No | Storage class of destination PVC (defaults to source storage class) |
| `--dest-storage-requests` | string | No | Requested storage capacity of destination PVC (defaults to source capacity) |
| `--destination-image` | string | No | Custom image to use for destination rsync Pod |
| `--source-image` | string | No | Custom image to use for source rsync Pod |
| `--endpoint` | string | No | Kind of endpoint to create in destination cluster (see [Endpoint Options](#endpoint-options)) |
| `--ingress-class` | string | When endpoint is nginx-ingress | Ingress class when endpoint is nginx-ingress |
| `--subdomain` | string | When endpoint is nginx-ingress | Custom subdomain to use for the endpoint |
| `--output` | string | No | Output transfer stats in the specified file |
| `--verify` | bool | No | Verify transferred files using checksums |
| `--cloud-storage` | string | No | S3-compatible cloud storage path for indirect transfer (e.g. remote:my-bucket) |
| `--rclone-config-secret` | string | No | Name of the K8s Secret containing rclone.conf for indirect transfer |
| `--rclone-config-file` | string | No | Path to local rclone.conf file for indirect transfer |
| `--encrypt` | bool | No | Enable client-side encryption for indirect transfer |
| `--keep-cloud-data` | bool | No | Reserved for cloud-data retention; currently has no effect because cleanup is not implemented |

### PVC Options

`--pvc-name` allows specifying a mapping of source and destination PVC names. This is a required option.

`--pvc-namespace=<namespace>` allows specifying a mapping of namespaces of source and destination PVC. By default, the namespaces in the source and destination contexts are used. When this option is specified, the namespaces in kube contexts are ignored and specified namespaces are used.

Both `--pvc-name` and `--pvc-namespace` follow mapping format `<source>:<destination>`, where `<source>` specifies the name in the source cluster while `<destination>` is the name in the destination cluster. If only `<source>` is specified, the same names are used in the destination cluster.

#### Examples

Transfer a PVC `test-pvc` in namespace `test-ns` to a destination PVC by the same name and namespace:

```bash
crane transfer-pvc --pvc-name=test-pvc --pvc-namespace=test-ns \
  --source-context=source --destination-context=destination --endpoint=route
```

Transfer a PVC `source-pvc` in namespace `source-ns` to a destination PVC `destination-pvc` in namespace `destination-ns`:

```bash
crane transfer-pvc --pvc-name=source-pvc:destination-pvc \
  --pvc-namespace=source-ns:destination-ns \
  --source-context=source --destination-context=destination --endpoint=route
```

Transfer a PVC to a new name within the same cluster and namespace (useful for storage class conversion):

```bash
crane transfer-pvc --source-context=mycluster --destination-context=mycluster \
  --pvc-name=mysql-data:mysql-data-new --pvc-namespace=myapp \
  --dest-storage-class=gp3 --endpoint=route
```

### Indirect Transfer Options

When using `--cloud-storage`, you must provide rclone credentials using either `--rclone-config-secret` (to point to an existing secret in the cluster) or `--rclone-config-file` (to provide a local configuration file that `crane` will convert into a temporary secret). These two flags are mutually exclusive.

### Endpoint Options

Endpoint enables a connection between the source and destination cluster for data transfer. It is created in the destination cluster. The destination cluster must support the kind of endpoint used.

By default, `nginx-ingress` is used as endpoint. For nginx-ingress, `--subdomain` and `--ingress-class` are required.

In an OpenShift cluster, `route` endpoint can be used. A subdomain option can be specified but is not required. By default, the cluster's subdomain will be used.

## Next Steps

After transferring PVC data, you may want to export, transform, and apply the remaining namespace resources:

```bash
crane export -n <namespace>
crane transform
crane apply
```

See [crane export](./export.md) for details.
