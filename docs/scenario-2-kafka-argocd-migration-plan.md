# Scenario #2: Migrate a stateful application managed by ArgoCD — understanding and plan

This document captures the understanding of Scenario #2 from the workload migration overview and a concrete plan to execute it.

---

## 1. Understanding of Scenario #2

### Goal

Migrate a **stateful application (Kafka)** that is **managed by ArgoCD** from cluster A to cluster B, using **two different strategies** so the team can compare them and document what works, what doesn’t, and where manual steps or other tools are needed.

### Why this scenario matters

- **ArgoCD-managed apps** are defined in Git; the “source of truth” is not the live cluster. Migration must account for:
  - Exporting/transforming only the right resources (or letting ArgoCD re-sync from Git on the target).
  - Whether to migrate ArgoCD `Application`/`ApplicationSet` resources or re-install ArgoCD and point it at the same (or updated) Git repo on the target.
- **Kafka is stateful**: data lives in PVCs. The scenario exercises **state migration**, not just moving manifests.
- **Two strategies** highlight different trade-offs:
  - **PVC-Transfer (Crane 2):** Move the workload by moving the PV data with `crane transfer-pvc`, then apply Kubernetes resources. Good when you want a “lift-and-shift” of the same storage.
  - **Application-specific replication (MirrorMaker 2):** Use Kafka’s native replication to sync data from source cluster to target, then cut over. Often the best path when the application supports it (minimizes custom migration logic and can support near-zero-downtime).

### Out of scope for this scenario

- Full automation of ArgoCD app discovery and re-bootstrap on target (we can document manual steps).
- Deep Kafka tuning (replication factor, retention, etc.); we use a deployable Kafka setup suitable for migration demos.

---

## 2. Prerequisites (to be confirmed/set up)

| Item | Description |
|------|-------------|
| **Two clusters** | Source (cluster A) and target (cluster B); OpenShift 4.x assumed (aligns with Scenario #1). |
| **ArgoCD** | Installed on source cluster (and optionally on target, or we install it as part of the scenario). |
| **Kafka deployment** | A Kafka deployment (e.g. Strimzi/AMQ Streams or another Kubernetes Kafka) **managed by ArgoCD** (app defined in a Git repo, synced by ArgoCD). |
| **Crane 2** | Built from this repo; `crane export`, `crane transform`, `crane transfer-pvc`, `crane apply` available. |
| **Kubeconfig** | Merged kubeconfig with `source` and `target` contexts (same pattern as Scenario #1). |
| **Namespace** | A dedicated namespace (or two) for Kafka on source; same or new namespace on target. |

---

## 3. Strategy A: PVC-Transfer (Crane 2)

**Idea:** Treat the migration like Scenario #1: export Kubernetes resources (including those created by ArgoCD/Kafka operator), transform them, migrate PVC data with `crane transfer-pvc`, then apply on the target. ArgoCD on the target can be configured later to manage the app from Git, or the app can be run as “manual” (applied by crane/oc) for the exercise.

### High-level steps

1. **Identify the workload**
   - Namespace(s) where Kafka is deployed (e.g. `kafka` or `argo-kafka`).
   - List PVCs used by Kafka (e.g. Kafka broker logs/data PVCs, ZooKeeper/KRaft metadata if applicable).
   - Optionally: use `crane export` with a namespace filter to capture all resources in that namespace (including CRDs from Strimzi/AMQ Streams).

2. **Quiesce / scale down Kafka on source (migration window)**
   - Scale Kafka brokers (and any Kafka Connect, etc.) to 0 or delete pods so that no process is writing to the PVCs.
   - This gives a consistent view of the volume for the final copy (aligns with the doc’s “quiesce → copy → unquiesce” pattern).

3. **Export resources**
   - Run `crane export -e ./export -n <kafka-namespace> --kubeconfig=<source-kubeconfig>` (and any other namespaces if Kafka spans multiple).

4. **Transform resources**
   - Run `crane transform -e ./export -t ./transform`.
   - If needed, add or document any **cluster-specific transforms** (e.g. storage class name, ingress/route annotations) so the manifests are valid on the target.

5. **Transfer PVCs**
   - For each Kafka (and optional ZooKeeper/KRaft) PVC, run:
     - `crane transfer-pvc --source-context=source --destination-context=target --pvc-name=<name> --pvc-namespace=<ns>:<ns> --endpoint=route`
   - Use merged kubeconfig so both contexts are available.
   - Document: PVC list, order (if any dependency), and any failures (e.g. size, storage class, permissions).

6. **Apply on target**
   - Create namespace on target if not present.
   - Run `crane apply -e ./export -t ./transform -o ./output`.
   - Apply generated manifests: `oc apply -f ./output/resources/<namespace>/ --namespace=<namespace> --context=target --kubeconfig=<merged>`.
   - If ArgoCD is used on target: either import the app into ArgoCD or point ArgoCD to the same Git repo and let it adopt the resources (document which approach was taken).

7. **Bring Kafka up on target**
   - Scale up or let the operator recreate pods; verify brokers see the existing data on the migrated PVCs.

8. **Validation**
   - Produce/consume test messages; verify topic data and offsets (as far as the scenario requires).

### Deliverables for Strategy A

- **Steps:** Numbered list of exact commands (and any manual edits) used.
- **Script:** Optional bash script (e.g. `scripts/migrate-kafka-pvc-transfer.sh`) that automates export, transform, transfer-pvc (for a configured list of PVCs), and apply.
- **Observations:** What worked; what didn’t (e.g. CRD version differences, storage class, permissions, ArgoCD re-sync conflicts); how you worked around it.

---

## 4. Strategy B: Application-specific replication (MirrorMaker 2)

**Idea:** Use Kafka’s MirrorMaker 2 (MM2) to replicate topics (and optionally configs, ACLs, offsets) from the source cluster’s Kafka to the target cluster’s Kafka. Then cut over clients to the target cluster. No `crane transfer-pvc` for Kafka data; Crane can still be used to migrate the Kubernetes resources (ArgoCD apps, Kafka CRs, configs, etc.) if desired.

### High-level steps

1. **Deploy Kafka on both clusters**
   - **Source:** Already running (ArgoCD-managed).
   - **Target:** Deploy a “target” Kafka cluster (can be ArgoCD-managed from the same or a different Git path). Ensure connectivity (network, TLS, auth) from source to target bootstrap service.

2. **Configure MirrorMaker 2**
   - Deploy MM2 (e.g. Strimzi `KafkaMirrorMaker2` resource or Kafka Connect with MM2 connectors) on source or on a dedicated cluster that can reach both Kafka clusters.
   - Configure:
     - Source cluster bootstrap servers and auth.
     - Target cluster bootstrap servers and auth.
     - Replication flow (e.g. source → target).
     - Topics to replicate (or replicate all).
   - References: [Red Hat – Mastering Kafka migration with MirrorMaker 2](https://developers.redhat.com/articles/2024/01/04/mastering-kafka-migration-mirrormaker-2), [Kafka MirrorMaker 2 documentation](https://kafka.apache.org/documentation/#georeplication).

3. **Stage (optional)**
   - Run MM2 for a period so that most topic data is already replicated to the target (best-effort; application still writing on source).

4. **Cutover**
   - Quiesce producers/consumers on source (or drain traffic).
   - Let MM2 catch up (e.g. consumer group offset sync if used).
   - Switch clients to the target cluster bootstrap.
   - Optionally decommission MM2 and source Kafka.

5. **Crane’s role (optional)**
   - Use Crane to export/transform/apply **non-Kafka-data** resources (e.g. ArgoCD Application for Kafka, ConfigMaps, Secrets, other apps in the same namespace). Kafka data is migrated by MM2, not by PVC transfer.

### Deliverables for Strategy B

- **Steps:** Numbered list of how you deployed Kafka on target, configured MM2, ran replication, and cut over.
- **Config snippets:** Key MM2 connector config (cluster aliases, replication flows, topics).
- **Observations:** What worked; what didn’t (e.g. auth, network, offset sync, CRD versions); links to Kafka Migration Guide (ArgoCD + MirrorMaker2) if we have one in the org.

---

## 5. Plan summary

| Phase | Action |
|-------|--------|
| **Setup** | Two OCP clusters; ArgoCD on source; deploy Kafka via ArgoCD in a dedicated namespace; note PVC names and sizes. |
| **Strategy A** | Follow “Strategy A: PVC-Transfer” above; document steps; optionally add `scripts/migrate-kafka-pvc-transfer.sh`; record issues and fixes. |
| **Strategy B** | Deploy Kafka on target; set up MirrorMaker 2; run replication and cutover; document steps and config; record issues and fixes. |
| **Document** | Single “Scenario 2 findings” doc (or extend this file): MTC comparison (if applicable), Crane 2 usage, manual workarounds, and when to prefer PVC-Transfer vs MM2. |

---

## 6. References

- Crane migration guide (this repo): [docs/crane-migration-guide.md](./crane-migration-guide.md)
- Crane transfer-pvc: [cmd/transfer-pvc/README.md](../cmd/transfer-pvc/README.md)
- Red Hat: [Mastering Kafka migration with MirrorMaker 2](https://developers.redhat.com/articles/2024/01/04/mastering-kafka-migration-mirrormaker-2)
- Kafka MirrorMaker 2: [Kafka documentation – Geo-replication](https://kafka.apache.org/documentation/#georeplication)
- Strimzi (Kafka on Kubernetes): [Strimzi documentation](https://strimzi.io/docs/operators/latest/overview.html)
- Internal: “Kafka Migration Guide: ArgoCD + MirrorMaker2” (if available in your org; link to be added)

---

## 7. Next steps

1. **Confirm environment:** Two clusters, ArgoCD, and a concrete Kafka app (e.g. Strimzi Kafka + optional MM2 CR) available or to be created.
2. **Create the ArgoCD + Kafka scenario:** One Git repo (or app of record) that defines Kafka (and optionally MM2) so the scenario is reproducible.
3. **Execute Strategy A:** Run PVC-Transfer path; capture exact commands and a script; document observations.
4. **Execute Strategy B:** Run MirrorMaker 2 path; capture config and steps; document observations.
5. **Write up:** Consolidate into “Scenario 2: Kafka ArgoCD – what we did and what we learned” (and link from the main migration overview doc).
