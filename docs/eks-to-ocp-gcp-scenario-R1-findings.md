# EKS-to-OCP on GCP: Scenario R1 (Service Types) — Findings

**Scope:** EKS (source) → OCP on GCP (oadp-5021). For EKS → OCP on AWS see [eks-to-ocp-resource-scenario-R1-findings.md](eks-to-ocp-resource-scenario-R1-findings.md).

**Scenario:** One Deployment + three Services (ClusterIP, NodePort, LoadBalancer). Script: `scripts/migrate-eks-to-ocp-scenario-r1.sh`. Workload: `scripts/scenario-r1-workload-gcp.yaml` (namespace `eks-ocp-scenario-r1-gcp`).

---

## Findings

### Export / Transform / Apply

- **Export (from EKS):** Succeeds. Exported Deployment, three Services, ConfigMap, ServiceAccount, ReplicaSet, Endpoints, EndpointSlices; no failures.
- **Transform:** Produces Deployment + three Services only (cluster-specific and ephemeral resources correctly whiteouted).
- **Apply to OCP:** All applied successfully. Namespace created; Deployment and all three Services created; pod reaches Running.

### Service types on OCP (GCP)

| Type         | Result on OCP (GCP) |
|--------------|----------------------|
| ClusterIP    | OK — ClusterIP assigned. |
| NodePort     | OK — NodePort assigned (e.g. 30080). |
| LoadBalancer | **Pending** — No EXTERNAL-IP at end of run. OCP on GCP may assign a GCP load balancer later depending on cloud provider integration; observed state was `<pending>`. |

### Pod

- Pod runs and is Ready (1/1) after apply. No post-apply changes required for the app to run.

### EKS authentication

- **Token-based auth** from an automated environment (e.g. Cursor runner) can fail with *"The token provided is invalid or expired"* even when the same token works in an interactive terminal — likely environment/network or validation differences.
- **Using existing kubeconfig** works: set `EKS_USE_EXISTING_KUBECONFIG=1` and `SOURCE_KUBECONFIG="${HOME}/.kube/config"` after logging in to EKS from your terminal. Successful run (2026-03-18) used this method.

### Script behavior

- With `set -euo pipefail`, the pipeline `kubectl config get-contexts -o name | head -1` can cause exit 141 (SIGPIPE) when the default kubeconfig has many contexts. The script was updated to use `|| true` and fallback to `kubectl config current-context` so the merge step completes.

---

## Findings summary

| Item | Result |
|------|--------|
| Export | OK (Deployment + 3 Services + supporting resources) |
| Transform | OK (Deployment + 3 Services in output) |
| Apply to OCP | OK (namespace created; all resources applied) |
| ClusterIP / NodePort on OCP | OK |
| LoadBalancer on OCP (GCP) | Pending (no EXTERNAL-IP observed) |
| Pod | Running |

---

## Run reference

Use GCP-specific namespace and dirs so results stay separate from AWS R1. Required: `OCP_CLUSTER_URL`, `OCP_KUBEADMIN_PASS`. For EKS: either `EKS_CLUSTER_NAME` + `EKS_REGION`, or `EKS_USE_EXISTING_KUBECONFIG=1` with `SOURCE_KUBECONFIG` pointing to your EKS kubeconfig. Optional overrides: `NAMESPACE`, `EXPORT_DIR`, `TRANSFORM_DIR`, `OUTPUT_DIR`, `WORKLOAD_YAML`. See script header in `scripts/migrate-eks-to-ocp-scenario-r1.sh` for full env vars.
