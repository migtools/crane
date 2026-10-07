# EKS-to-OCP Resource Scenario R3: StorageClass and PVC References — Findings

## Overview

Scenario R3 tests migration of a **Deployment with a PVC** using `storageClassName: gp3` (EKS EBS) from EKS to OCP using crane export → transform → apply. No transfer-pvc in this scenario (resource-only; volume on OCP is new/empty if it binds).

**Script:** `scripts/migrate-eks-to-ocp-scenario-r3.sh`  
**Workload:** `scripts/scenario-r3-workload.yaml`

## Environment

| Property   | EKS (Source) | OCP (Target) |
|-----------|----------------|---------------|
| Cluster   | EKS (us-east-1) | OCP on AWS (oadp-4851) |
| Namespace | eks-ocp-scenario-r3 | eks-ocp-scenario-r3 |

## Setup on EKS

- **PVC:** `scenario-r3-data` — 1Gi, ReadWriteOnce, **storageClassName: gp3** (EBS CSI on EKS).
- **Deployment:** `scenario-r3-app` — single replica, volume mount `/data` from PVC `scenario-r3-data`.

## Run Status

**Note:** A first run failed at **EKS login** (token likely expired). The script and workload are in place. Re-run with fresh credentials:

```bash
export EKS_CLUSTER_URL="https://<your-eks-api>"
export EKS_TOKEN="<fresh-token>"
export OCP_CLUSTER_URL="https://api.oadp-4851.qe.devcluster.openshift.com:6443"
export OCP_KUBEADMIN_PASS="<password>"
./scripts/migrate-eks-to-ocp-scenario-r3.sh
```

Below is the **expected** behavior based on the plan and existing EKS exports (e.g. scenario-2 PVC with gp3).

## Expected Export

- **Command:** `crane export -e ./export-eks-ocp-r3 -n eks-ocp-scenario-r3 --kubeconfig=<EKS kubeconfig>`
- **Exported resources:** ConfigMaps, Endpoints, EndpointSlices, Pods, ServiceAccounts, **PersistentVolumeClaims**, Deployments, ReplicaSets, etc.
- **PVC in export:** The PVC YAML will include:
  - `spec.storageClassName: gp3`
  - EBS-specific annotations (e.g. `volume.kubernetes.io/storage-provisioner: ebs.csi.aws.com`, `volume.kubernetes.io/selected-node`, `pv.kubernetes.io/bind-completed`)
  - Possibly `spec.volumeName` and `status` (phase: Bound) from the source cluster.

## Expected Transform

- The default Kubernetes plugin does **not** rewrite `spec.storageClassName` or strip EBS annotations. So the applied PVC on OCP will still request **gp3**.
- Controller-managed/ephemeral resources (ConfigMap, Endpoints, EndpointSlices, Pod, ReplicaSet, ServiceAccount) are whiteouted as in R1/R2.
- **Output:** Deployment, PVC (with gp3 and possibly some EBS annotations), and any other non-whiteouted resources.

## Expected Apply to OCP

- **If OCP has a StorageClass named `gp3` (or default that satisfies the claim):** PVC can bind; Deployment pod can start and mount the volume (empty, since no transfer-pvc).
- **If OCP has only a different name (e.g. `gp3-csi`):** PVC may stay **Pending** with a “storage class not found” or “no provisioner” type message until the manifest is edited or a transform maps `gp3` → `gp3-csi` (or target default).
- **EBS annotations:** Harmless on OCP if the provisioner is different; OCP may ignore them. Some (e.g. `volumeName`) might need to be stripped for a new provisioner to bind the claim.

## Recommendations

1. **Storage class mapping:** Use a transform plugin (or optional flag) that rewrites `spec.storageClassName` from `gp3` (and optionally `gp2`) to a configurable OCP storage class (e.g. `gp3-csi` or the cluster default). Optionally strip EBS-specific annotations and `spec.volumeName` so the PVC is treated as a new claim on the target.
2. **Apply order:** The R3 script applies PVC first, then Deployment, so the PVC has a chance to bind before the pod is created.
3. **With transfer-pvc:** For real data migration, use `crane transfer-pvc` with `--dest-storage-class` for the destination PVC; the exported PVC manifest can still be transformed to the same storage class so that after apply the transferred volume matches the claim.

## Summary

| Item | Expected / Note |
|------|------------------|
| Export | PVC exported with storageClassName: gp3 and EBS annotations |
| Transform | No storage-class change by default; PVC in output as gp3 |
| Apply to OCP | Success if OCP has gp3 (or equivalent); otherwise PVC may stay Pending |
| Transform improvement | Plugin to map gp3 → OCP storage class and optionally strip EBS/volumeName |

Scenario R3 script and workload are ready; re-run with valid EKS credentials and then update this doc with actual export/transform/apply and PVC status on OCP.
