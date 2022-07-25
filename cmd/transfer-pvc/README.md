# Transfer Persistent Volume Claims

The `transfer-pvc` subcommand in Crane can be used to transfer _PersistentVolumeClaim_ resource and volume data to destination cluster. It establishes connection to the destination cluster by creating a public endpoint of user's choice in the destination namespace. It then creates a PVC and an _rsync_ daemon Pod in the destination namespace to receive data from the source PVC. Finally, it creates an _rsync_ client Pod in the source namespace which transfers data to the rsync daemon using the endpoint. The connection is encrypted using self-signed cerificates created automatically at the time of transfer.

## Example

```bash
crane transfer-pvc --source-context=<source> --destination-context=<destination> --pvc-name=<pvc_name> --endpoint=route
```

The above command transfers PVC (along with PV data) named `<pvc_name>` in the namespace specified by `<source>` context into the namespace specified by `<destination>` context. The `--endpoint` argument specifies the kind of public endpoint to use to establish a connection between the source and the destination cluster.

## Options

`transfer-pvc` subcommand can be configured using various additional options available:

| Option                | Type    | Required | Description                                                                                   |
|-----------------------|---------|----------|-----------------------------------------------------------------------------------------------|
| source-context        | string  | Yes      | Kube context of the source cluster                                                            |
| destination-context   | string  | Yes      | Kube context of the destination cluster                                                       |
| pvc-name              | string  | Yes      | Mapping of the source/destination PVC names (See [PVC options](#pvc-options))                 |
| pvc-namespace         | string  | No       | Mapping of the source/destination PVC namespaces (See [PVC options](#pvc-options))            |
| dest-storage-class    | string  | No       | Storage class of destination PVC (Defaults to source storage class)                           |
| dest-storage-requests | string  | No       | Requested storage capacity of destination PVC (Defaults to source capacity)                   |
| destination-image     | string  | No       | Custom image to use for destination rsync Pod                                                 |
| source-image          | string  | No       | Custom image to use for source rsync Pod                                                      |
| endpoint              | string  | No       | Kind of endpoint to create in destination cluster (See [Endpoint Options](#endpoint-options)) |
| ingress-class         | string  | No       | Ingress class when endpoint is nginx-ingress (See [Endpoint Options](#endpoint-options))      |
| subdomain             | string  | No       | Custom subdomain to use for the endpoint (See [Endpoint Options](#endpoint-options))          |
| output                | string  | No       | Output transfer stats in the specified file                                                   |
| verify                | bool    | No       | Verify transferred files using checksums                                                      |
| help                  | bool    | No       | Display help                                                                                  |

### PVC Options

`--pvc-name` option allows specifying a mapping of source and destination PVC names. This is a required option.

`--pvc-namespace=<namespace>` option allows specifying a mapping of namespaces of source and destination PVC. By default, the namespaces in the source and destination contexts are used. When this option is specified the namespaces in kube contexts are ignored and specified namespaces are used.

Both `--pvc-name` and `--pvc-namespaces` follow mapping format `<source>:<destination>`, where `<source>` specifies the name in the source cluster while `<destination>` is the name in the destination cluster. If only `<source>` is specified, the same names are used in destination cluster.

#### Examples

To transfer a PVC `test-pvc` in namespace `test-ns` to a destination PVC by same name & namespace: 

```bash
crane transfer-pvc --pvc-name=test-pvc --pvc-namespace=test-ns ...
```

To transfer a PVC `source-pvc` in namespace `source-ns` to a destination PVC `destination-pvc` in namespace `destination-ns`:

```bash
crane transfer-pvc --pvc-name=source-pvc:destination-pvc --pvc-namespace=source-ns:destination-ns ...
```

### Endpoint Options

Endpoint enables a connection between the source and destination cluster for data transfer. It is created in the destination cluster. The destination cluster _must_ support the kind of endpoint used.

By default, `nginx-ingress` is used as endpoint. For nginx-ingress, `--subdomain` and `--ingress-class` are required. 

In an OpenShift cluster, `route` endpoint can be used. A subdomain option can be specified but is not required. By default, the cluster's subdomain will be used.


