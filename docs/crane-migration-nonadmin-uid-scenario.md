# Crane migration: non-admin UID/GID volume scenario

This guide describes a **demonstration scenario** for [kubectl-migrate Issue #60](https://github.com/konveyor-ecosystem/kubectl-migrate/issues/60): migrating a PVC whose data is owned by **non-root UIDs/GIDs**, when the workload runs under **restricted SCC** (OpenShift). The scenario deploys a custom app that writes data with restrictive file permissions, runs the full crane migration, then compares file ownership and permissions on the target to see whether they are preserved across namespaces with different UID ranges.

---

## 1. What this scenario tests

On OpenShift, each namespace gets a unique **UID range** (e.g. `openshift.io/sa.scc.uid-range: 1000680000/10000`). Under **restricted SCC**, pods run as a UID from that range. When the app writes to the PVC, files are owned by that namespace-allocated UID.

**The problem:** Source and target namespaces have **different** UID ranges. During migration:

- The **rsync client** pod on the source runs as the source namespace UID and can read the files.
- The **rsync server** pod on the target runs as the target namespace UID and writes the copied data.
- Crane sets `RunAsUser`, `RunAsGroup`, and `FSGroup` from the namespace annotations (see `getIDsForNamespace` in `cmd/transfer-pvc/transfer-pvc.go`).

**Questions the scenario answers:**

1. Does rsync preserve the original UID/GID ownership, or do files end up owned by the target namespace UID?
2. Can the app pod on the target (running as the target namespace UID) access the migrated files?
3. Are file permissions (e.g. `0700`) preserved?

Results are written under `nonadmin-uid-results/` (source and target ownership, UID range annotations) so you can compare.

---

## 2. Prerequisites

- **OpenShift CLI:** `oc` and `kubectl` (or set `OC_BIN` / `KUBECTL_BIN`)
- **Crane:** Built by the script from this repo, or set `CRANE_BIN`
- **User-creation script:** `improved_user.sh`; set `IMPROVED_USER_SCRIPT` (default: `$HOME/Documents/oadp/improved_user.sh`)
- **Cluster access:** Kubeadmin credentials for both source and target OpenShift clusters
- **Default storage:** 1Gi RWO PVC (uses cluster default StorageClass; e.g. GCP PD or NFS)

All paths and credentials can be overridden with environment variables (see table at the end).

---

## 3. What the script does

The script **`scripts/migrate-pvcs-nonadmin-uid.sh`** runs:

| Step | Action |
|------|--------|
| 1 | Build crane binary |
| 2 | Log in as kubeadmin; create user and namespace on both clusters via `improved_user.sh` (admin role) |
| 2 | Wait for OAuth; switch to created user; merge kubeconfigs (contexts `source` / `target`) |
| 3 | Record namespace UID range annotations for source and target (as kubeadmin) |
| 4 | Deploy custom app on source: 1 PVC (`nonadmin-data`), 1 Deployment (no explicit `runAsUser`; restricted SCC assigns namespace UID). App writes data to `/data/testfile` with `chmod 0700` and records `id` and `ls -lan /data/` to `/data/ownership-info`. |
| 5 | Record source file ownership (from `/data/ownership-info` and `ls -lan /data/`) |
| 6–10 | Run **crane export**, **transform**, **transfer-pvc** (single PVC), **crane apply**, **oc apply** to target |
| 11 | Wait for target pod; record target ownership (pod writes `id` and `ls -lan /data/` to `/data/target-report.txt` only if data already exists, so migrated files are not overwritten). Compare and print source vs target ownership and UID ranges. |

The custom app uses a single PVC and a minimal Deployment (e.g. `ubi8/ubi-minimal`) so no appm or extra tooling is required.

---

## 4. How to run the scenario

From the **repo root**:

```bash
./scripts/migrate-pvcs-nonadmin-uid.sh
```

Defaults: namespace `nonadmin-uid-ns`, user `nonadmin-uid-user`, PVC `nonadmin-data`. Override as needed:

```bash
SOURCE_CLUSTER_URL=https://api.cam-src-70680.qe.devcluster.openshift.com:6443 \
TARGET_CLUSTER_URL=https://api.cam-tgt-70680.qe.devcluster.openshift.com:6443 \
SOURCE_KUBEADMIN_PASS='<source-password>' \
TARGET_KUBEADMIN_PASS='<target-password>' \
USER_NAME=nonadmin-uid-user \
NAMESPACE=nonadmin-uid-ns \
./scripts/migrate-pvcs-nonadmin-uid.sh
```

---

## 5. Expected results and output

- **Source:** The script prints the source pod’s `id` and `ls -lan /data/` (ownership and permissions before migration). These are also saved under `nonadmin-uid-results/source-ownership.txt` and `nonadmin-uid-results/source-uid-range.txt`.
- **Target:** After migration, the script prints the target pod’s `id` and `ls -lan /data/` (ownership of the migrated files). Saved under `nonadmin-uid-results/target-ownership.txt` and `nonadmin-uid-results/target-uid-range.txt`.
- **Comparison:** The script prints a short comparison block and the path to `nonadmin-uid-results/`. Use this to see:
  - Whether file ownership on the target matches the source UID/GID or the target namespace UID.
  - Whether permissions (e.g. `0700`) are preserved.
  - Whether the target app can read the migrated files (the target pod runs the same image and only reports ownership when `/data/testfile` already exists).

Interpretation depends on storage type (e.g. block vs NFS): with block storage, `fsGroup` can change effective ownership at mount time; with NFS, ownership is whatever was written and is not adjusted by Kubernetes. The scenario does not change cluster storage; it only records and compares ownership for the default StorageClass.

---

## 6. Summary

| Item | Description |
|------|-------------|
| **Purpose** | Test migration of PVC data owned by non-root (namespace) UID under restricted SCC; compare ownership before/after. |
| **Script** | [scripts/migrate-pvcs-nonadmin-uid.sh](../scripts/migrate-pvcs-nonadmin-uid.sh) |
| **User creation** | Uses `improved_user.sh` (admin role) on both clusters. |
| **App** | Custom Deployment + 1 PVC; writes data with restrictive perms; no appm. |
| **Output** | `nonadmin-uid-results/` (source/target ownership and UID ranges). |

---

## 7. Script configuration (environment variables)

| Variable | Default | Description |
|----------|---------|-------------|
| `NAMESPACE` | `nonadmin-uid-ns` | Namespace created on both clusters |
| `USER_NAME` | `nonadmin-uid-user` | User created (admin in namespace) |
| `USER_PASS` | `P@ssWord` | User password |
| `PVC_NAME` | `nonadmin-data` | Name of the single PVC to migrate |
| `SOURCE_CLUSTER_URL`, `TARGET_CLUSTER_URL` | (script defaults) | Cluster API URLs |
| `SOURCE_KUBEADMIN_PASS`, `TARGET_KUBEADMIN_PASS` | (script defaults) | Kubeadmin passwords |
| `IMPROVED_USER_SCRIPT` | `$HOME/Documents/oadp/improved_user.sh` | User/namespace creation script |
| `EXPORT_DIR` | `./export-nonadmin-uid` | Crane export directory |
| `TRANSFORM_DIR` | `./transform-nonadmin-uid` | Crane transform directory |
| `OUTPUT_DIR` | `./output-nonadmin-uid` | Crane apply output directory |
| `MERGED_KUBECONFIG` | `/tmp/merged-kubeconfig-nonadmin-uid` | Merged kubeconfig path |

Other variables (`OC_BIN`, `KUBECTL_BIN`, `GO_BIN`, `CRANE_BIN`, `ENDPOINT_TYPE`, `WAIT_USER_TIMEOUT`, `WAIT_POD_TIMEOUT`, and kubeconfig paths) are also overridable; see the script header.

---

## 8. References

- **Issue #60:** [Migrate a volume containing data owned by non-admin group](https://github.com/konveyor-ecosystem/kubectl-migrate/issues/60)
- **Main migration guide:** [crane-migration-guide.md](crane-migration-guide.md)
- **Crane transfer-pvc:** Uses namespace UID/supplemental-groups annotations in `cmd/transfer-pvc/transfer-pvc.go` (`getIDsForNamespace`, `getRsyncClientPodSecurityContext`, `getRsyncServerPodSecurityContext`)
