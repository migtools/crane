# crane transfer-pvc

Transfer PersistentVolumeClaim resources and volume data between clusters.

## Synopsis

```bash
crane transfer-pvc [flags]
```

## Description

The `transfer-pvc` subcommand transfers a PersistentVolumeClaim resource and its volume data to a destination cluster. It supports two primary transfer modes:

1. **Direct Mode**: Establishes a direct connection between source and destination clusters by creating a public endpoint (e.g., `route` or `ingress`) in the destination namespace. An rsync client Pod in the source transfers data directly to an rsync daemon Pod in the destination.
2. **Indirect Mode**: Enables transfers between clusters without direct network connectivity. Data is uploaded to cloud storage by a source Pod and then downloaded to the destination PVC by a destination Pod.

`transfer-pvc` supports transfers between different clusters or within the same cluster. When performing transfers within the same cluster and namespace, the source and destination PVC names must be different.

Indirect mode can also be run as a **split workflow** using `--upload-only` and `--download-only` when the source and target clusters cannot be accessed from the same Crane invocation. See [Split Indirect Transfer Workflow](#split-indirect-transfer-workflow) for details.

## Example

### Direct Transfer
```bash
crane transfer-pvc --source-context=<source> --destination-context=<destination> --pvc-name=<pvc_name> --endpoint=route
```

### Indirect Transfer (via Cloud Storage)
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
| `--source-context` | string | Complete and upload-only workflows | Kube context of the source cluster |
| `--destination-context` | string | Complete and download-only workflows | Kube context of the destination cluster |
| `--pvc-name` | string | Yes | Mapping of source/destination PVC names (see [PVC Options](#pvc-options)) |
| `--pvc-namespace` | string | No | Mapping of source/destination PVC namespaces (see [PVC Options](#pvc-options)) |
| `--dest-storage-class` | string | No | Storage class of destination PVC (defaults to source PVC's storage class; **required** when destination lacks a class by that name, e.g. cross-provider transfers). See [Cross-provider transfers](#cross-provider-transfers). |
| `--dest-storage-requests` | string | No | Requested storage capacity of destination PVC (defaults to source capacity) |
| `--destination-image` | string | No | Custom image to use for destination rsync Pod |
| `--source-image` | string | No | Custom image to use for source rsync Pod |
| `--endpoint` | string | No | Kind of endpoint to create in destination cluster (see [Endpoint Options](#endpoint-options)) |
| `--ingress-class` | string | When endpoint is nginx-ingress | Ingress class when endpoint is nginx-ingress |
| `--subdomain` | string | When endpoint is nginx-ingress | Custom subdomain to use for the endpoint |
| `--output` | string | No | Output transfer stats in the specified file |
| `--verify` | bool | No | Verify transferred files using checksums |
| `--cloud-storage` | string | No | rclone cloud storage path for indirect transfer (e.g. remote:my-bucket) |
| `--rclone-config-secret` | string | No | Name of a Secret containing `rclone.conf` in the namespaces required by the selected workflow |
| `--rclone-config-file` | string | No | Local path to `rclone.conf`; crane creates temporary Secrets in each cluster it contacts |
| `--encrypt` | bool | No | Enable client-side encryption for indirect transfer |
| `--keep-cloud-data` | bool | No | Retain the uploaded object prefix after the transfer instead of cleaning it up |
| `--upload-only` | bool | No | Upload source PVC data to cloud storage without contacting the destination cluster; requires `--source-context`, `--cloud-storage`, and `--keep-cloud-data` |
| `--download-only` | bool | No | Download cloud storage data into an existing destination PVC without contacting the source cluster; requires `--destination-context` and `--cloud-storage` |
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

### Cross-provider transfers

When crane provisions the destination PVC, it defaults the destination PVC's storage class to the **source PVC's** storage class unless you override it with `--dest-storage-class`.

When the source and destination are on different infrastructure providers (for example AWS→GCP, or any two clusters that do not share storage-class names), you **must** pass `--dest-storage-class` with a class that exists on the destination. Storage class names like `gp3-csi` (AWS) and `standard-csi` (GCP) do not overlap, the inherited source class does not exist on the destination. List the destination's classes with `kubectl --context <destination> get storageclass` and pass an appropriate one:

```bash
crane transfer-pvc \
  --source-context aws-cluster --destination-context gcp-cluster \
  --pvc-name mysql-data \
  --pvc-namespace myapp:myapp \
  --dest-storage-class standard-csi \
  --cloud-storage remote:my-bucket --rclone-config-secret rclone-secret
```

This applies to both direct and indirect transfers.

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

Indirect transfer enables PVC migration between clusters without direct network connectivity. Data is uploaded to cloud storage by the source cluster and then downloaded by the destination cluster. The transfer image must include the rclone backend selected by the remote; Azure Blob Storage requires the `azureblob` backend.

#### Complete Indirect Transfer Workflow

Use the complete workflow when one Crane invocation can access both clusters. Crane reads the source PVC, creates the destination PVC, uploads the data to cloud storage, downloads it to the destination PVC, and cleans up the cloud data.

```bash
crane transfer-pvc \
  --source-context source \
  --destination-context destination \
  --pvc-name source-pvc:destination-pvc \
  --pvc-namespace source-ns:destination-ns \
  --cloud-storage remote:bucket/transfer-prefix \
  --rclone-config-file rclone.conf
```

`--keep-cloud-data` is optional in the complete workflow. It retains the uploaded object prefix after a successful transfer.

#### Split Indirect Transfer Workflow

Use the split workflow when the source and target clusters cannot be accessed from the same Crane invocation. The upload and download commands use the same `--cloud-storage` value and source PVC identity.

**Phase 1 — Upload from the source cluster:**

```bash
crane transfer-pvc \
  --upload-only \
  --source-context source \
  --pvc-name source-pvc:destination-pvc \
  --pvc-namespace source-ns:destination-ns \
  --cloud-storage remote:bucket/transfer-prefix \
  --rclone-config-file source-rclone.conf \
  --keep-cloud-data
```

`--destination-context` is intentionally omitted: `--upload-only` does not contact the target cluster. The destination half of the PVC mapping is not used during upload, but it keeps the two commands consistent.

Crane uploads the data to:

```text
remote:bucket/transfer-prefix/source-ns/source-pvc
```

`--keep-cloud-data` is required so the uploaded data remains available for the later download.

**Phase 2 — Create the destination PVC:**

Apply only the rendered target PVC manifest from the normal `crane export` / `crane transform`  / `crane apply` output. The PVC must exist before download because `--download-only` cannot read the source PVC or create the destination PVC.

```bash
kubectl --context destination apply \
  -f output/resources/destination-ns/PersistentVolumeClaim__v1_destination-ns_destination-pvc.yaml
```

The rendered manifest preserves transformed target settings such as storage class, capacity, access modes, name, and namespace. Wait for the PVC to be ready to mount, and keep workloads that use it scaled down until the restore finishes.

**Phase 3 — Download to the target cluster:**

```bash
crane transfer-pvc \
  --download-only \
  --destination-context destination \
  --pvc-name source-pvc:destination-pvc \
  --pvc-namespace source-ns:destination-ns \
  --cloud-storage remote:bucket/transfer-prefix \
  --rclone-config-file target-rclone.conf
```

`--source-context` is intentionally omitted: `--download-only` does not contact the source cluster. The source PVC name and namespace locate the cloud data, while the destination PVC name and namespace identify the existing target PVC.

After a successful download, cloud data is cleaned up by default. Pass `--keep-cloud-data` only if you need to retain the data for a retry. Then apply or scale up the remaining rendered workload resources.

**Workflow rules:**

- `--upload-only` and `--download-only` are mutually exclusive.
- The complete workflow requires both `--source-context` and `--destination-context`.
- `--upload-only` requires `--source-context`, `--cloud-storage`, `--keep-cloud-data`, and an rclone configuration; it does not use `--destination-context`.
- `--download-only` requires `--destination-context`, `--cloud-storage`, an rclone configuration, and an existing destination PVC; it does not use `--source-context`.
- Use the same `--cloud-storage` value and source PVC identity in both split commands. Avoid a trailing slash in the prefix because Crane appends `/source-namespace/source-pvc-name`.
- The upload and download commands may use different rclone credentials if both configurations define the same remote name and can access the same effective path.
- Automatic `--encrypt` is not supported in the split workflow because its generated crypt configuration is ephemeral. Use a stable user-managed rclone crypt remote with compatible passwords, salt, and encryption settings if encryption is required.

#### Configuration

When using `--cloud-storage`, provide rclone configuration using **one** of the following mutually exclusive options. For a complete indirect transfer, the rclone remote named in `--cloud-storage` must be usable from Pods in **both** clusters and must grant each side access to the same bucket and prefix. For a split workflow, the upload configuration must work from Pods in the source cluster and the download configuration must work from Pods in the destination cluster.

- `--rclone-config-file` — A local file on the machine running `crane`. Crane reads it, creates a temporary Secret in each PVC namespace it contacts, validates that Secret, and deletes it when the command finishes. A complete transfer contacts both clusters; `--upload-only` contacts only the source cluster and `--download-only` contacts only the destination cluster. No manually created Secrets are needed.
- `--rclone-config-secret` — The name of an existing Secret. For a complete transfer, create that Secret with the same name in both clusters: once in the source PVC namespace and once in the destination PVC namespace. For a split workflow, create it in the source PVC namespace for upload and in the destination PVC namespace for download. Each Secret must contain a non-empty `rclone.conf` key, and Crane does not copy or create a user-managed Secret.

The two configurations may use different credentials when appropriate, but both `rclone.conf` files must define the remote name used by `--cloud-storage` and their credentials must be authorized for the transfer path. If the path is wrapped by a user-managed rclone crypt remote, both configurations must use compatible passwords or passphrases, salt, and encryption settings.

For example, for `--cloud-storage remote:my-bucket/transfer-path`, both configurations need a `[remote]` section. A MinIO endpoint expressed as an in-cluster DNS name is normally reachable only from that cluster; use an endpoint that is reachable from Pods on both clusters.

Azure Blob Storage supports storage account key and service principal authentication. The following examples use a remote named `azure`; the remote name is arbitrary, and `type = azureblob` selects the backend.

```ini
[azure]
type = azureblob
account = <storage-account>
key = <storage-account-key>
```

A storage-account key can access all data in the storage account.

Service principal authentication uses the tenant ID, client ID, and client secret from an Azure service principal that has the `Storage Blob Data Contributor` role on the target container. That role permits access to all blobs in the container:

```ini
[azure]
type = azureblob
account = <storage-account>
tenant = <tenant-id>
client_id = <client-id>
client_secret = <client-secret>
```

Then use the remote name, container, and transfer prefix in `--cloud-storage`:

```bash
crane transfer-pvc \
  --source-context source --destination-context destination \
  --pvc-name data-pvc \
  --pvc-namespace source-ns:destination-ns \
  --cloud-storage azure:my-container/transfer-prefix \
  --rclone-config-file /secure/path/rclone.conf
```

The storage-account-key example can access all data in the storage account. The service-principal example can access all blobs in the target container; it is not restricted to `transfer-prefix`. To restrict the service principal to `transfer-prefix`, add an Azure ABAC condition for both the blob `path` and listing `prefix`. Keep `rclone.conf` out of source control.

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
- **File ownership**: Both transfer modes preserve file **contents**, **permissions (mode bits)**, and **directory structure**. Original file ownership (UID/GID) is generally **not** preserved when the mover Pod runs as non-root, which is the norm. See [Note: file ownership (UID/GID) is normalized on transfer](#note-file-ownership-uidgid-is-normalized-on-transfer) below.

##### Note: file ownership (UID/GID) is normalized on transfer

Preserving original UID/GID requires the receiving process to run as root, which crane's mover Pods normally do not. As a result, restored files generally do **not** keep their original owner/group. This affects both transfer modes, and changing UIDs is expected when moving data between clusters. The two modes differ in *how* ownership is resolved:

- **Direct mode** runs rsync with `--owner`/`--group`. As root these preserve the original UID/GID; as a non-root Pod (the common case) rsync cannot restore arbitrary owners and silently maps files to the UID it runs as. The transfer still succeeds.
- **Indirect mode** explicitly rewrites ownership. Internally (not a user-facing option), crane sets ownership metadata on every downloaded file so files always match the resolved download security context, not the original owner. The values are derived as follows:
  - **UID** is `RunAsUser` when it is set, otherwise it falls back to **65534 (nobody)**.
  - **GID** is `RunAsGroup` when set, otherwise `FSGroup` when set, otherwise the resolved UID above (so **65534** only when `RunAsUser` is also unset).

  This is intentional: rclone treats a failed `chown` as a fatal error and discards the file, and a non-root mover Pod cannot restore an arbitrary source UID/GID, so ownership is normalized to let the transfer complete.

**Impact:** In most cases the migrated workload can access its data regardless of the exact owning UID, so no action is needed. Ownership only matters for workloads that require a specific owner — for example, a database that expects its data directory owned by a service UID, or files that must be group-readable by a specific GID — which may fail to start or misbehave until ownership is corrected on the destination.

**If your workload requires specific ownership:**
- When the workload needs to **own** the files, run it with `runAsUser` set to the mover Pod's file UID (see the indirect-mode UID rule above).
- When the workload only needs **group access** to the files, set `fsGroup` to the mover Pod's file GID (see the indirect-mode GID rule above); note that `fsGroup` changes group ownership and grants group access, not file UID ownership.
- `chown` the restored data on the destination PVC before starting the workload.

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

For the split indirect workflow, see [Split Indirect Transfer Workflow](#split-indirect-transfer-workflow).

## Next Steps

After transferring PVC data, you may want to export, transform, and apply the remaining namespace resources:

```bash
crane export -n <namespace>
crane transform
crane apply
```

See [crane export](./export.md) for details.
