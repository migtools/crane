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

### Indirect Transfer (via S3)
```bash
crane transfer-pvc --source-context=source --destination-context=destination \
  --pvc-name=data-pvc \
  --cloud-storage=remote:my-bucket/transfer-path \
  --rclone-config-secret=rclone-secret
```

See [Indirect Transfer Options](#indirect-transfer-options) for detailed configuration.


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
| `--audit-log` | string | No | Path to the audit log file (defaults to `audit/.crane-audit.log`) |

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

For the complete end-to-end workflow including workload reference updates, see the [StorageClass Conversion Guide](../storageclass-conversion.md).

> **Warning — StorageClass conversion with StatefulSets:** `crane transfer-pvc` migrates data from existing PVCs to new PVCs on the target StorageClass, but it does not modify the StatefulSet's `volumeClaimTemplates`. If the StatefulSet is scaled up after conversion without being recreated, new replicas will provision PVCs on the original StorageClass. To complete the conversion, delete the StatefulSet with `--cascade=orphan` (preserving existing pods and PVCs) and recreate it with the updated `storageClassName` in the `volumeClaimTemplates` spec.

### Audit Logging

`crane` maintains a persistent, structured JSON Lines audit log of all operations. This file is located at `audit/.crane-audit.log` by default.

To save the audit log to a different location:
```bash
crane transfer-pvc --audit-log=/tmp/crane-transfer.log ...
```

> **Note:** The `audit/` directory is automatically created if it does not exist. It is recommended to add `audit/` to your `.gitignore` file to avoid tracking these logs in version control.


### Endpoint Options

Endpoint enables a connection between the source and destination cluster for data transfer. It is created in the destination cluster. The destination cluster must support the kind of endpoint used.

By default, `nginx-ingress` is used as endpoint. For nginx-ingress, `--subdomain` and `--ingress-class` are required.

In an OpenShift cluster, `route` endpoint can be used. A subdomain option can be specified but is not required. By default, the cluster's subdomain will be used.

### Indirect Transfer Options

Indirect transfer enables PVC migration between clusters without direct network connectivity. Data is uploaded to an S3-compatible cloud storage bucket by the source cluster and then downloaded by the destination cluster.

#### Configuration

When using `--cloud-storage`, you must provide rclone credentials using **one** of the following (mutually exclusive):

- `--rclone-config-secret` — Name of an existing Kubernetes Secret containing `rclone.conf` in the cluster
- `--rclone-config-file` — Path to a local `rclone.conf` file (crane creates a temporary Secret automatically)

#### Behavior

- **Data Retention**: The `--keep-cloud-data` flag is reserved for cloud-data retention; currently has no effect because cleanup is not implemented.
- **Encryption**: Use `--encrypt` to enable client-side encryption for data in transit.

When `--encrypt` is used:
1. You must provide the configuration via `--rclone-config-file`. This flag cannot be used with `--rclone-config-secret`.
2. `crane` automatically generates a secure, ephemeral 32-byte encryption password for the transfer session.
3. The password is obscured using rclone's native AES-CTR format and appended to the configuration as an `[encrypted]` crypt overlay section.
4. The generated configuration is used to create temporary secrets on both clusters, ensuring secure end-to-end encryption. The password is discarded after the transfer completes.

#### Sample rclone.conf

**MinIO (self-hosted):**
```ini
[remote]
type = s3
provider = Minio
access_key_id = <minio-access-key>
secret_access_key = <minio-secret-key>
endpoint = http://minio.minio.svc.cluster.local:9000
```

**AWS S3:**
```ini
[remote]
type = s3
provider = AWS
access_key_id = <aws-access-key-id>
secret_access_key = <aws-secret-access-key>
region = us-east-1
```

**GCS (S3-compatible mode):**
```ini
[remote]
type = s3
provider = GCS
access_key_id = <gcs-access-key-id>
secret_access_key = <gcs-secret-access-key>
endpoint = https://storage.googleapis.com
```

The section name `[remote]` must match the prefix in `--cloud-storage`. For example, `--cloud-storage "remote:my-bucket"` uses the `[remote]` section.

#### Examples

Basic indirect transfer:
```bash
crane transfer-pvc \
  --source-context source --destination-context destination \
  --pvc-name data-pvc \
  --cloud-storage remote:my-bucket/transfer-path \
  --rclone-config-secret rclone-secret
```

With encryption and data retention:
```bash
crane transfer-pvc \
  --source-context source --destination-context destination \
  --pvc-name data-pvc \
  --cloud-storage remote:my-bucket/transfer-path \
  --rclone-config-file rclone.conf \
  --encrypt \
  --keep-cloud-data
```

## Next Steps

After transferring PVC data, you may want to export, transform, and apply the remaining namespace resources:

```bash
crane export -n <namespace>
crane transform
crane apply
```

See [crane export](./export.md) for details.
