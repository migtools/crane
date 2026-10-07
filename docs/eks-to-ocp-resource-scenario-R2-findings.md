# EKS-to-OCP Resource Scenario R2: Ingress (ALB-style) — Findings

## Overview

Scenario R2 tests migration of a workload with **Deployment + ClusterIP Service + Ingress** (ALB IngressClass and annotations) from EKS to OCP using crane export → transform → apply. No PVC; resource-only.

**Script:** `scripts/migrate-eks-to-ocp-scenario-r2.sh`  
**Workload:** `scripts/scenario-r2-workload.yaml`

## Environment

| Property   | EKS (Source) | OCP (Target) |
|-----------|----------------|---------------|
| Cluster   | EKS (us-east-1) | OCP on AWS (oadp-4851) |
| Namespace | eks-ocp-scenario-r2 | eks-ocp-scenario-r2 |

## Setup on EKS

- **Deployment:** `scenario-r2-app` — single replica, ubi8/ubi-minimal, containerPort 8080.
- **Service:** `scenario-r2-web` — ClusterIP, port 80 → 8080.
- **Ingress:** `scenario-r2-ingress` — `ingressClassName: alb`, annotations `alb.ingress.kubernetes.io/scheme: internet-facing`, `alb.ingress.kubernetes.io/target-type: ip`, rule path `/` → service `scenario-r2-web:80`.

On EKS at listing time the Ingress showed CLASS `alb`, ADDRESS empty (ALB may still be provisioning or AWS Load Balancer Controller may not be installed; the resource was created).

## Export

- **Command:** `crane export -e ./export-eks-ocp-r2 -n eks-ocp-scenario-r2 --kubeconfig=<EKS kubeconfig>`
- **Exported resources:** ConfigMaps, Endpoints, EndpointSlices, Pods, ServiceAccounts, Services, Deployments, ReplicaSets, **Ingresses**.
- **Failures:** None.

## Transform

- **Command:** `crane transform -e ./export-eks-ocp-r2 -t ./transform-eks-ocp-r2`
- **Whiteouted (same as R1):** ConfigMap, EndpointSlice, Endpoints, Pod, ReplicaSet, ServiceAccount.
- **Not whiteouted:** **Deployment, Service, Ingress** — all three were written to output. The default Kubernetes plugin does **not** whiteout Ingress.

## Apply to OCP

- Namespace created; then `oc apply -f output-eks-ocp-r2/resources/eks-ocp-scenario-r2/` for Deployment, Ingress, and Service.
- **Outcome:** All applied successfully.

| Resource   | Apply result |
|-----------|---------------|
| Deployment | created; pod Running |
| Service (scenario-r2-web) | created, ClusterIP |
| Ingress (scenario-r2-ingress) | created |

## Observation: Ingress on OCP

On OCP the Ingress object exists but **has no ADDRESS**:

- **CLASS:** `alb`
- **HOSTS:** `*`
- **ADDRESS:** _(empty)_
- **PORTS:** 80

OCP does not run the AWS ALB Ingress Controller, so no controller reconciles this Ingress to create an ALB or Route. The Ingress is valid YAML and applies cleanly, but it does **not** provide external access on OCP.

## Recommendations

1. **Apply behavior:** Applying the exported Ingress to OCP succeeds; the resource is created but has no effect on routing. Deployment and Service work as expected.
2. **Transform option — whiteout Ingress:** For EKS→OCP, consider a transform plugin (or optional flag) that **whiteouts** Ingress resources with `ingressClassName: alb` (or similar EKS-specific class) and optionally writes a note (e.g. to a README in the output dir) to "create an OpenShift Route for external access."
3. **Transform option — Ingress → Route:** A plugin could generate an OpenShift **Route** manifest from the Ingress spec (name, host, path, backend service) so that after apply the user gets a Route YAML to apply instead of (or in addition to) the Ingress. OpenShift would then reconcile the Route and expose the service.
4. **Manual step:** After migration, create a Route pointing to `scenario-r2-web` (and drop or ignore the Ingress) for external access on OCP.

## Summary

| Item | Result |
|------|--------|
| Export | Ingress exported with Deployment and Service; no failures |
| Transform | Ingress not whiteouted; Deployment, Service, Ingress in output |
| Apply to OCP | Success for all three |
| Ingress on OCP | Object created; ADDRESS empty (no ALB controller) |
| Pod | Running; app reachable via Service only (no external Ingress) |

Scenario R2 complete. Ingress applies but does not provide external access on OCP; use a Route or a transform that whiteouts/generates Route for EKS→OCP.
