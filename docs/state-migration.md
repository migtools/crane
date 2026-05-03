# State Migration

This document explains how Crane transfers persistent volume data between Kubernetes clusters using `crane transfer-pvc`.

## Overview

Persistent volume data cannot be migrated through YAML manifests alone. Crane uses rsync over an encrypted stunnel connection to transfer the actual data from source PVCs to destination PVCs.

## How It Works

```text
Source Cluster                              Destination Cluster
┌─────────────────┐                        ┌─────────────────┐
│ rsync client Pod │──── stunnel ──────────▶│ rsync daemon Pod│
│ (reads source    │    (encrypted)         │ (writes to dest │
│  PVC data)       │         │              │  PVC)           │
└─────────────────┘         │              └─────────────────┘
                            │              ┌─────────────────┐
                            └─────────────▶│ Public endpoint  │
                                           │ (Route/Ingress)  │
                                           └─────────────────┘
```

### Transfer Process

1. **Destination PVC creation** — Crane creates a PVC on the destination cluster matching the source PVC spec (storage class and size can be overridden)
2. **Endpoint setup** — A public endpoint (OpenShift Route or nginx Ingress) is created on the destination cluster
3. **TLS certificate generation** — Self-signed certificates are automatically generated for encrypted transport
4. **rsync daemon deployment** — An rsync daemon Pod is created in the destination namespace, mounting the destination PVC
5. **stunnel transport** — A stunnel server is set up on the destination to terminate TLS
6. **rsync client execution** — An rsync client Pod is created on the source cluster, mounting the source PVC and transferring data through the stunnel connection
7. **Cleanup** — All transfer resources (Pods, Secrets, endpoints) are removed after completion

## Endpoint Types

### OpenShift Route

```bash
crane transfer-pvc --endpoint route ...
```

- Uses OpenShift's built-in Route with passthrough TLS
- No additional configuration required
- Subdomain is optional (uses cluster default)
- Destination cluster must be OpenShift

### nginx Ingress

```bash
crane transfer-pvc --endpoint nginx-ingress --subdomain transfer.example.com --ingress-class nginx ...
```

- Uses standard Kubernetes Ingress with nginx controller
- Requires `--subdomain` and `--ingress-class` flags
- Destination cluster must have an nginx ingress controller
- Works with any Kubernetes distribution

## Security

- All data transfer is encrypted via stunnel (TLS)
- Self-signed certificates are generated per transfer
- Certificates are stored as Kubernetes Secrets and cleaned up after transfer
- rsync Pods run with restricted security contexts:
  - `allowPrivilegeEscalation: false`
  - `runAsNonRoot: true`
  - Capabilities dropped: `ALL`
  - Seccomp profile: `RuntimeDefault`

## Performance Considerations

- **Network bandwidth**: Transfer speed is limited by the network path between clusters
- **PVC size**: Large volumes may take significant time; monitor progress via rsync output
- **Node affinity**: The rsync client Pod is scheduled on the same node as the Pod currently mounting the source PVC
- **Checksum verification**: Use `--verify` for data integrity at the cost of additional time
- **Concurrent transfers**: Run multiple `crane transfer-pvc` commands in parallel for different PVCs

## Name and Namespace Mapping

Source PVC names and namespaces can be mapped to different values on the destination:

```bash
# Same name, same namespace
crane transfer-pvc --pvc-name my-data --pvc-namespace my-app ...

# Different name
crane transfer-pvc --pvc-name source-data:dest-data ...

# Different namespace
crane transfer-pvc --pvc-namespace source-ns:dest-ns ...
```

## Long PVC Names

PVC names exceeding 63 characters (Kubernetes limit) are automatically handled by Crane, which generates an MD5 hash-based name for the transfer resources.

## Further Reading

- [crane transfer-pvc Reference](./commands/transfer-pvc.md) — Full command reference
- [Stateful Migration Tutorial](./stateful-migration-tutorial.md) — Step-by-step tutorial
- [backube/pvc-transfer](https://github.com/backube/pvc-transfer) — Underlying library
