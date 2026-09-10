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
| `--rclone-config-secret` | string | No | Name of a Secret containing `rclone.conf` in both transfer namespaces |
| `--rclone-config-file` | string | No | Local path to `rclone.conf`; crane creates temporary Secrets on both clusters |
| `--encrypt` | bool | No | Enable client-side encryption for indirect transfer |
| `--keep-cloud-data` | bool | No | Retain the uploaded object prefix after the transfer instead of cleaning it up |
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

When using `--cloud-storage`, provide rclone configuration using **one** of the following mutually exclusive options. In either case, the rclone remote named in `--cloud-storage` must be usable from Pods in **both** clusters and must grant each side access to the same bucket and prefix.

- `--rclone-config-file` — A local file on the machine running `crane`. Crane reads it, creates temporary Secrets containing it in the source and destination PVC namespaces, validates both Secrets, and deletes those temporary Secrets when the command finishes. No manually created Secrets are needed. The identity running Crane needs permission to create, read, and delete Secrets in both namespaces.
- `--rclone-config-secret` — The name of an existing Secret. You must create that Secret in **both clusters**: once in the source PVC namespace and once in the destination PVC namespace. The Secrets must have the same name (the one passed to the flag) and each must contain a non-empty `rclone.conf` key. Crane does not copy or create a user-managed Secret.

The two Secrets may use different credentials when appropriate, but both `rclone.conf` files must define the remote name used by `--cloud-storage` and their credentials must be authorized for the transfer path.

For example, for `--cloud-storage remote:my-bucket/transfer-path`, both configurations need a `[remote]` section. A MinIO endpoint expressed as an in-cluster DNS name is normally reachable only from that cluster; use an endpoint that is reachable from Pods on both clusters.

#### Using a local config file

```bash
crane transfer-pvc \
  --source-context source --destination-context destination \
  --pvc-name data-pvc \
  --pvc-namespace source-ns:destination-ns \
  --cloud-storage remote:my-bucket/transfer-path \
  --rclone-config-file /secure/path/rclone.conf
```

`rclone.conf` is not read from either cluster. It is read locally by the Crane CLI and its contents are made available to both transfer Pods through temporary Secrets.

#### Using pre-created Secrets

Create the Secret in each PVC namespace before starting the transfer. The Secret name is deliberately identical because the command accepts one name:

```bash
kubectl --context source -n source-ns create secret generic rclone-secret \
  --from-file=rclone.conf=/secure/path/source-rclone.conf

kubectl --context destination -n destination-ns create secret generic rclone-secret \
  --from-file=rclone.conf=/secure/path/destination-rclone.conf
```

Then run:

```bash
crane transfer-pvc \
  --source-context source --destination-context destination \
  --pvc-name data-pvc \
  --pvc-namespace source-ns:destination-ns \
  --cloud-storage remote:my-bucket/transfer-path \
  --rclone-config-secret rclone-secret
```

Do not use `--rclone-config-file` with `--rclone-config-secret`.

#### Behavior

- **Data Retention**: By default, Crane runs a cloud cleanup after download. `--keep-cloud-data` skips that cleanup and leaves the transferred object prefix in the bucket. Cleanup failures are reported as non-fatal warnings.
- **Encryption**: `--encrypt` enables rclone client-side encryption of data stored in the intermediate bucket. It is separate from transport encryption and any bucket-side encryption.
- **File ownership**: Indirect mode preserves file **contents**, **permissions (mode bits)**, and **directory structure**, but **not** file ownership (UID/GID). See [Limitation: file ownership is not preserved](#limitation-file-ownership-uidgid-is-not-preserved) below.

##### Limitation: file ownership (UID/GID) is not preserved

In indirect mode, restored files do **not** keep their original owner/group. On download, files are written with the UID/GID of the destination (mover) Pod; when that Pod runs with no explicit `runAsUser`, ownership defaults to **65534 (nobody)**.

This is intentional. rclone treats a failed `chown` as a fatal error and discards the file, and a non-root mover Pod cannot restore an arbitrary source UID/GID, so ownership is normalized to the Pod's own identity to let the transfer complete. Direct mode (rsync) is not affected by this limitation.

**Impact:** workloads that depend on specific file ownership — for example, a database that expects its data directory owned by a service UID, or files that must be group-readable by a specific GID — may fail to start or misbehave after an indirect transfer until ownership is corrected on the destination.

**Workarounds:**
- Run the destination workload with an `fsGroup`/`runAsUser` that matches the mover-Pod identity.
- `chown` the restored data on the destination PVC before starting the workload.
- Use direct mode (`--endpoint`) when file ownership must be preserved.

`--encrypt` applies only to indirect (`--cloud-storage`) transfers. It does not change direct rsync transfers: direct mode sends rsync traffic through Crane's TLS stunnel tunnel, so its network traffic is already encrypted in transit. Indirect mode has no direct cluster-to-cluster rsync connection; `--encrypt` protects the temporary bucket objects and their names with rclone crypt.

When `--encrypt` is used:
1. You must provide the configuration via `--rclone-config-file`. This flag cannot be used with `--rclone-config-secret`.
2. `crane` automatically generates a secure, ephemeral 32-byte encryption password for that transfer session.
3. The password is obscured using rclone's native AES-CTR format and appended to the configuration as an `[encrypted]` crypt overlay section.
4. The generated configuration is used to create temporary Secrets on both clusters. The password is discarded after the transfer completes.

> **Note:** Because the automatic password is new for every invocation, do not combine automatic `--encrypt` with repeat runs that retain cloud data for rclone incrementality. Use bucket-side encryption, or supply a stable user-managed rclone `crypt` configuration without `--encrypt`, for that use case.

#### Encrypted incremental transfers with a stable user-managed key

To retain client-side encryption *and* let rclone reuse unchanged data across repeat transfers, define the `crypt` remote yourself and keep its password unchanged. Generate an obscured value once with a standard rclone installation:

```bash
rclone obscure 'choose-a-strong-stable-password'
```

Store the output as the `password` value below. `rclone obscure` prevents accidental plaintext disclosure in the file; the resulting value must still be treated as a credential.

```ini
[remote]
type = s3
provider = AWS
access_key_id = <access-key-id>
secret_access_key = <secret-access-key>
region = <region>

[encrypted]
type = crypt
remote = remote:<bucket>/<stable-transfer-prefix>
password = <output-from-rclone-obscure>
```

Invoke Crane with the crypt remote and **omit** `--encrypt`:

```bash
crane transfer-pvc \
  --source-context source --destination-context destination \
  --pvc-name data-pvc \
  --pvc-namespace source-ns:destination-ns \
  --cloud-storage encrypted: \
  --rclone-config-file /secure/path/stable-rclone.conf \
  --keep-cloud-data
```

Crane appends the source namespace and PVC name below `encrypted:`. Since the `[encrypted]` remote, backing bucket prefix, and crypt password remain stable, rclone can identify unchanged encrypted objects and uploads only added or changed files. Changed files transfer in full; rclone does not perform block-level deltas through object storage.

If using `--rclone-config-secret` instead of `--rclone-config-file`, create the same stable crypt configuration in the Secret on both clusters as described above. Do not change the `password` or backing `remote` path between repeat transfers.

#### Sample rclone.conf

**MinIO (self-hosted, endpoint reachable from both clusters):**
```ini
[remote]
type = s3
provider = Minio
access_key_id = <minio-access-key>
secret_access_key = <minio-secret-key>
endpoint = https://minio.example.com
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

Basic indirect transfer using pre-created Secrets (create the Secret on both clusters as shown above):
```bash
crane transfer-pvc \
  --source-context source --destination-context destination \
  --pvc-name data-pvc \
  --cloud-storage remote:my-bucket/transfer-path \
  --rclone-config-secret rclone-secret
```

With automatic encryption and retained cloud data:
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
