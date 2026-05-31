# crane transfer-pvc

Transfer PersistentVolumeClaim resources and volume data between clusters.

## Synopsis

```bash
crane transfer-pvc [flags]
```

## Description

The `transfer-pvc` subcommand transfers a PersistentVolumeClaim resource and its volume data to a destination cluster. It establishes a connection to the destination cluster by creating a public endpoint of the user's choice in the destination namespace. It then creates a PVC and an rsync daemon Pod in the destination namespace to receive data from the source PVC. Finally, it creates an rsync client Pod in the source namespace which transfers data to the rsync daemon using the endpoint. The connection is encrypted using self-signed certificates created automatically at the time of transfer.

## Example

```bash
crane transfer-pvc --source-context=<source> --destination-context=<destination> --pvc-name=<pvc_name> --endpoint=route
```

The above command transfers PVC (along with PV data) named `<pvc_name>` in the namespace specified by `<source>` context into the namespace specified by `<destination>` context. The `--endpoint` argument specifies the kind of public endpoint to use to establish a connection between the source and the destination cluster.

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
| `--ingress-class` | string | No | Ingress class when endpoint is nginx-ingress |
| `--subdomain` | string | No | Custom subdomain to use for the endpoint |
| `--output` | string | No | Output transfer stats in the specified file |
| `--verify` | bool | No | Verify transferred files using checksums |

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
