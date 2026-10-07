# EKS-to-OCP Resource Scenario R1: Service Types — Findings (OCP on AWS)

**Target:** OCP on AWS (oadp-4851). For EKS → OCP on GCP see [eks-to-ocp-gcp-scenario-R1-findings.md](eks-to-ocp-gcp-scenario-R1-findings.md).

## Overview

Scenario R1 tests migration of a workload with **one Deployment and three Services** (ClusterIP, NodePort, LoadBalancer) from EKS to OCP using crane export → transform → apply. No PVC or transfer-pvc; resource-only.

**Script:** `scripts/migrate-eks-to-ocp-scenario-r1.sh`  
**Workload:** `scripts/scenario-r1-workload.yaml`

## Environment

| Property   | EKS (Source) | OCP (Target) |
|-----------|----------------|---------------|
| Cluster   | EKS (us-east-1) | OCP on AWS (us-east-2, oadp-4851) |
| Namespace | eks-ocp-scenario-r1 | eks-ocp-scenario-r1 |

## Setup on EKS

- **Deployment:** `scenario-r1-app` — single replica, ubi8/ubi-minimal, sleep 3600.
- **Services:**
  - `scenario-r1-clusterip` — type ClusterIP, port 8080.
  - `scenario-r1-nodeport` — type NodePort, port 8080, nodePort 30080.
  - `scenario-r1-loadbalancer` — type LoadBalancer, port 80 → 8080.

On EKS, the LoadBalancer service received an AWS ELB: `aacd1a207e61149fd9bdd85b4c558c94-1202567082.us-east-1.elb.amazonaws.com`.

## Export

- **Command:** `crane export -e ./export-eks-ocp-r1 -n eks-ocp-scenario-r1 --kubeconfig=<EKS kubeconfig>`
- **Exported resources:** ConfigMaps, Endpoints, EndpointSlices, Pods, ServiceAccounts, Services (all 3), Deployments, ReplicaSets.
- **Failures:** None (`export/failures/eks-ocp-scenario-r1/` empty).

## Transform

- **Command:** `crane transform -e ./export-eks-ocp-r1 -t ./transform-eks-ocp-r1`
- **Built-in Kubernetes plugin (crane-lib) whiteouted:**
  - ConfigMap `kube-root-ca.crt`
  - All EndpointSlices and Endpoints (controller-managed)
  - Pod (ephemeral)
  - ReplicaSet (let Deployment own replicas on target)
  - ServiceAccount `default` (target creates default SA with namespace)
- **Result:** Only **Deployment** and **three Services** were written to `output/`.

## Apply to OCP

- Namespace created; then `oc apply -f output-eks-ocp-r1/resources/eks-ocp-scenario-r1/` for the four files.
- **Outcome:** All applied successfully.

| Resource   | Apply result |
|-----------|---------------|
| Deployment | created; pod reached Running |
| scenario-r1-clusterip (ClusterIP) | created |
| scenario-r1-nodeport (NodePort)   | created, nodePort 30080 |
| scenario-r1-loadbalancer (LoadBalancer) | created; **EXTERNAL-IP assigned** |

## Observation: LoadBalancer on OCP

On this **OCP-on-AWS** (ROSA or similar) target cluster, the LoadBalancer Service was **provisioned with an AWS load balancer**:

- **EXTERNAL-IP:** `a0ec44b4eafaa477e904debb331aa5bd-1779447902.us-east-2.elb.amazonaws.com` (us-east-2, matching target cluster region).
- So for **EKS → OCP on AWS**, applying the same LoadBalancer Service YAML did **not** require changing type to ClusterIP; the target cloud provider (AWS) satisfied the LoadBalancer type.

For **OCP on-prem or non-AWS**, LoadBalancer would typically stay in `<pending>` unless an on-prem load balancer integration exists. In that case, a transform to change type to ClusterIP and document “create Route” would be useful.

## Recommendations

1. **OCP on AWS:** LoadBalancer Services can be applied as-is; they will get an AWS ELB/NLB. No transform required for type.
2. **OCP on-prem / non-AWS:** Consider a transform plugin or patch that changes `type: LoadBalancer` to `type: ClusterIP` and adds a comment or annotation (e.g. `crane.transform/create-route: "true"`) so operators know to create an OpenShift Route for external access.
3. **NodePort:** Applied and worked; nodePort 30080 preserved. No change needed.
4. **Transform whiteouts:** ConfigMap/Endpoints/EndpointSlices/Pod/ReplicaSet/ServiceAccount whiteout is appropriate; keeps apply output to the minimal set (Deployment + Services).

## Summary

| Item | Result |
|------|--------|
| Export | All relevant resources exported; no failures |
| Transform | Only Deployment + 3 Services in output; rest whiteouted |
| Apply to OCP | Success for Deployment, ClusterIP, NodePort, LoadBalancer |
| LoadBalancer on OCP (AWS) | Received EXTERNAL-IP (AWS ELB) |
| Pod | Running on OCP |

Scenario R1 complete. LoadBalancer behavior is cluster-dependent (works on OCP-on-AWS; may stay pending on non-AWS OCP).
