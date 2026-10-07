# Scenario #3: Network volume migration with non-admin UID/GID under restricted SCC/PSA

This document describes migration scenarios for **NFS (network) volumes** with data owned by non-admin UIDs/GIDs, when workloads run under **restricted SCC** and **PSA enforcement** on OpenShift. Network volumes behave fundamentally differently from block storage for UID/GID handling during migration.

---

## Why network volumes are different

On block storage (GCP PD, AWS EBS), Kubernetes uses `fsGroup` to `chown` all files at mount time -- ownership is automatically remapped to the pod's UID/GID. On **NFS and CephFS**, `fsGroup` only adds a supplemental group; it does **not** `chown` files. File ownership on NFS is whatever the writing process used.

This distinction matters for migration because:

- **Crane's rsync runs without `--owner`/`--group`** (disabled by `restrictedContainers(true)` in `cmd/transfer-pvc/transfer-pvc.go`), so files on the destination are owned by the rsync server's UID, not the original source UID.
- On block storage, `fsGroup` would re-chown migrated files at mount time on the target. On NFS, it does not -- files keep whatever UID the rsync server wrote them as.
- Source and target namespaces have different UID ranges (e.g. `1000740000` vs `1000780000`), so the rsync server writes files as the target namespace UID.

---

## Scenario A: NFS + Restricted SCC -- Namespace-Allocated UID (Baseline)

### What this scenario tests

The simplest network-volume case: the app does **not** set explicit `runAsUser`/`runAsGroup`; the restricted SCC assigns a UID from the namespace annotation range. Crane detects these UIDs via `getIDsForNamespace()` and sets them on the rsync client/server pods.

### Questions answered

1. Does rsync successfully transfer data on NFS under restricted SCC?
2. Are file ownership UIDs remapped to the target namespace UID (since `--owner`/`--group` are disabled)?
3. Are file permission mode bits (0644, 0700, 0750, 0755) preserved?
4. Does the target app pod have access to all migrated files?

### App design

- 1 PVC using NFS StorageClass (`managed-nfs-storage`), `ReadWriteMany`
- 1 Deployment with **no explicit** `securityContext.runAsUser` (restricted SCC assigns namespace UID)
- Container writes files with multiple permission levels: `0644`, `0700`, `0750`, `0755`
- Also writes a nested subdirectory with a file
- Records `id` and `ls -lan /data/` to `/data/ownership-info`

### Results

Run against:
- **Source:** OpenShift 4.15.61 (oadp-3861), NFS server `18.117.140.85`
- **Target:** OpenShift 4.18.34 (oadp-3851), NFS server `18.220.196.43`
- **NFS provisioner:** `nfs-subdir-external-provisioner` v4.0.2

**Source (before migration):**

| Property | Value |
|----------|-------|
| Pod UID | `1000740000` (namespace-allocated) |
| Pod GID | `0` (root), supplemental group `1000740000` |
| SCC | `restricted-v2` |
| File ownership | All files owned by `1000740000:0` |
| Permissions | `0644`, `0700`, `0750`, `0755` as set |

```
uid=1000740000 gid=0(root) groups=0(root),1000740000

-rw-r--r--. 1 1000740000 0  testfile-0644
-rwx------. 1 1000740000 0  testfile-0700
-rwxr-x---. 1 1000740000 0  testfile-0750
-rwxr-xr-x. 1 1000740000 0  testfile-0755
drwxr-xr-x. 2 1000740000 0  subdir/
-rwxr-x---. 1 1000740000 0  subdir/nested-file
```

**Target (after migration):**

| Property | Value |
|----------|-------|
| Pod UID | `1000780000` (different namespace UID) |
| Pod GID | `0` (root), supplemental group `1000780000` |
| File ownership | All files re-owned to `1000780000:0` |
| Permissions | **Preserved** -- same mode bits as source |
| File read test | **ALL files readable** (0644, 0700, 0750, 0755) |

```
uid=1000780000 gid=0(root) groups=0(root),1000780000

-rw-r--r--. 1 1000780000 0  testfile-0644
-rwx------. 1 1000780000 0  testfile-0700
-rwxr-x---. 1 1000780000 0  testfile-0750
-rwxr-xr-x. 1 1000780000 0  testfile-0755
drwxr-xr-x. 2 1000780000 0  subdir/
-rwxr-x---. 1 1000780000 0  subdir/nested-file
```

### Key findings

1. **Migration succeeds.** Crane `transfer-pvc` correctly transfers all files from source NFS to target NFS via rsync.
2. **File ownership is remapped** from source namespace UID (`1000740000`) to target namespace UID (`1000780000`). This happens because rsync runs without `--owner`/`--group`, so the rsync server writes files as its own UID (target namespace UID). On NFS, unlike block storage, `fsGroup` does not re-chown files at mount time -- the files simply end up owned by whatever UID wrote them (the rsync server).
3. **Permission bits are preserved.** All mode bits (`0644`, `0700`, `0750`, `0755`) survive the rsync transfer unchanged.
4. **Target app can read ALL files.** Because the rsync server remapped ownership to the target namespace UID, and the target app pod also runs as that UID, all files are accessible -- including `0700` (owner-only) files. This is the desired behavior.
5. **Nested directories and files** are transferred correctly with preserved structure and permissions.
6. **Cross-version migration** (OCP 4.15 to 4.18) works without issues.

### Interpretation

For the **namespace-allocated UID** pattern (no explicit `runAsUser`), NFS migration with Crane works correctly. The fact that rsync **does not** preserve original ownership is actually beneficial here: the rsync server re-writes files with the target namespace UID, which matches the UID the target app pod runs as. This avoids the access problem that would occur if the original source UID were preserved (since the target pod has a different UID).

This contrasts with block storage, where `fsGroup` handles re-chowning at mount time. On NFS, the rsync UID remapping achieves the same effect.

---

## Scenario B: NFS + Explicit Non-Root UID/GID (Common Workload Pattern)

### What this scenario tests

Many real-world images (PostgreSQL, MySQL, NGINX, Node.js) run as a specific hardcoded non-root user (e.g. UID 1001). The pod spec sets `runAsUser: 1001, runAsGroup: 1001, fsGroup: 1001`. Files on NFS are owned by `1001:1001`.

Crane's rsync pods do NOT use the app's UID -- they use the **namespace-allocated UID** from annotations (e.g. `1000760000`). This means:

- The rsync client runs as UID `1000760000`, not `1001`
- The rsync client may not be able to read files owned by UID `1001` with restrictive permissions (0700, 0750)
- Even if transfer succeeds, files on the target are re-owned to the rsync server's namespace UID, not the app's UID

### Questions answered

1. Can crane migrate NFS data when the app uses a hardcoded UID different from the namespace-allocated UID?
2. Does the rsync client fail to read restrictively-permissioned files?
3. After migration, does the target app (running as the hardcoded UID) have access to migrated files?

### App design

- 1 PVC using NFS StorageClass (`managed-nfs-storage`), `ReadWriteMany`
- 1 Deployment with explicit `securityContext`: `runAsUser: 1001, runAsGroup: 1001, fsGroup: 1001`
- Requires `nonroot-v2` SCC (granted to the default SA as kubeadmin)
- Container writes files with multiple permission levels: `0644`, `0700`, `0750`, `0755`
- Records `id` and `ls -lan /data/` to `/data/ownership-info`

### Results

Run against the same clusters as Scenario A.

**Source (before migration):**

| Property | Value |
|----------|-------|
| Pod UID | `1001` (explicit, hardcoded) |
| Pod GID | `1001` (explicit, hardcoded) |
| SCC | `nonroot-v2` |
| File ownership | All files owned by `1001:1001` |
| Permissions | `0644`, `0700`, `0750`, `0755` as set |

```
uid=1001 gid=1001 groups=1001

-rw-r--r--. 1 1001 1001  testfile-0644
-rwx------. 1 1001 1001  testfile-0700
-rwxr-x---. 1 1001 1001  testfile-0750
-rwxr-xr-x. 1 1001 1001  testfile-0755
drwxr-xr-x. 2 1001 1001  subdir/
-rwxr-x---. 1 1001 1001  subdir/nested-file
```

**Transfer status: Partially failed (50%)**

```
Status: Partially failed
Progress:
  Percentage: 50%
  Transferred: 778.00 bytes
  Files:
    Sent: 19
    Total: 7
```

**Target (after migration):**

| Property | Value |
|----------|-------|
| Pod UID | `1001` (same explicit UID on target) |
| SCC | `nonroot-v2` |
| Transfer status | **Partially failed** |

The target shows **mixed ownership** -- a critical finding:

```
-rw-r--r--. 1 1000790000    0  ownership-info    <- re-owned (world-readable, transferred)
-rw-r--r--. 1 1000790000    0  testfile-0644     <- re-owned (world-readable, transferred)
-rwx------. 1       1001 1001  testfile-0700     <- ORIGINAL UID (owner-only, partially failed)
-rwxr-x---. 1       1001 1001  testfile-0750     <- ORIGINAL UID (owner+group, partially failed)
-rwxr-xr-x. 1 1000790000    0  testfile-0755     <- re-owned (world-readable, transferred)
drwxr-xr-x. 2 1000790000    0  subdir/           <- directory transferred, but empty
```

| File | Source UID | Target UID | Mode | Transfer result |
|------|-----------|------------|------|----------------|
| testfile-0644 | 1001 | **1000790000** | 0644 (world-readable) | Transferred, re-owned to namespace UID |
| testfile-0700 | 1001 | **1001** | 0700 (owner-only) | Partially failed; original UID retained |
| testfile-0750 | 1001 | **1001** | 0750 (owner+group) | Partially failed; original UID retained |
| testfile-0755 | 1001 | **1001** | 0755 (world-readable) | Transferred, re-owned to namespace UID |
| subdir/nested-file | 1001 | _(missing)_ | 0750 | **Not transferred** |

### Key findings

1. **Transfer partially failed.** Crane's `transfer-pvc` reported "Partially failed" with only 50% of data transferred. The rsync client (running as namespace UID `1000760000`) could not read files with restrictive permissions (0700, 0750) owned by UID `1001`.

2. **World-readable files transferred successfully but re-owned.** Files with mode `0644` and `0755` were transferred because the rsync client could read them (world-readable bits). On the target, they are owned by the rsync server's namespace UID (`1000790000`), not the app's UID (`1001`).

3. **Restrictively-permissioned files were not fully transferred.** Files with mode `0700` and `0750` appeared on the target with their original UID (`1001:1001`), indicating rsync could not properly transfer them. The nested file (`subdir/nested-file`, mode `0750`) was **not transferred at all**.

4. **Mixed ownership on target creates fragility.** The target volume has files owned by two different UIDs (`1000790000` and `1001`). The target app (running as UID `1001`) can read its own files (0700/0750) but can only read the re-owned files if they are world-readable (0644/0755).

5. **SCC RoleBinding export issue.** Crane exported the `nonroot-v2` SCC RoleBinding from the source namespace. When applying to the target as a non-admin user, this fails with a permission error. The workaround is to grant the SCC on the target as kubeadmin before applying manifests, and remove the SCC RoleBinding files from the crane output.

### Interpretation

This scenario demonstrates a **real limitation** of crane's `transfer-pvc` for NFS volumes when the app uses a hardcoded UID different from the namespace-allocated UID. Crane always sets rsync pod UIDs from namespace annotations (`getIDsForNamespace()`), not from the workload's actual security context. On NFS, where file-level Unix permissions are enforced, this UID mismatch prevents rsync from reading owner-only or group-only files.

**Potential mitigations:**
- Add a `--rsync-uid` / `--rsync-gid` flag to `transfer-pvc` to allow users to specify the UID/GID for rsync pods, matching the workload's UID.
- Use `rsync-anyuid` SCC for the rsync pods so they can read all files regardless of ownership (requires elevated permissions).
- Before migration, relax file permissions on the source volume to world-readable (not always acceptable).

---

## Scenario C: NFS + Supplemental Groups for Shared Access

### What this scenario tests

A common NFS pattern for multi-pod access: the volume is shared across pods/services using a common GID (e.g. `5000`). The pod spec sets `supplementalGroups: [5000]`, and files are created with group `5000` and group-level permissions (`0770`, `0660`). Any pod with that supplemental group can read/write shared data.

Crane's rsync pods do NOT set `supplementalGroups` from the workload's spec -- they only use namespace annotations. However, the rsync client UID matches the file **owner** UID (both are namespace-allocated), so the transfer may succeed even without the shared GID. The key question is whether the shared **group ownership** survives migration.

### Questions answered

1. Does rsync transfer succeed when files have a shared GID (5000) and group-level permissions?
2. Is the shared GID (5000) preserved after migration?
3. Does the target app retain shared-access semantics (can a pod with only supplementalGroups: [5000] access the data)?

### App design

- 1 PVC using NFS StorageClass (`managed-nfs-storage`), `ReadWriteMany`
- 1 Deployment with **no explicit** `runAsUser` (namespace UID assigned), `supplementalGroups: [5000]`
- Admitted under `restricted-v2` SCC (supplementalGroups uses RunAsAny strategy)
- Container creates files with `chgrp 5000` and group permissions: `0770`, `0660`, `0664`
- Also creates owner-only (`0700`) and world-readable (`0644`) files as controls
- Creates a shared subdirectory (`shared-dir`) with GID 5000 and mode `0770`

### Results

Run against the same clusters as Scenarios A and B.

**Source (before migration):**

| Property | Value |
|----------|-------|
| Pod UID | `1000770000` (namespace-allocated) |
| Pod GID | `0` (root), supplemental groups `5000`, `1000770000` |
| SCC | `restricted-v2` |
| File ownership | Files owned by `1000770000:5000` (shared GID) or `1000770000:0` |

```
uid=1000770000 gid=0(root) groups=0(root),5000,1000770000

-rw-r--r--. 1 1000770000    0  testfile-0644
-rw-rw----. 1 1000770000 5000  testfile-0660
-rw-rw-r--. 1 1000770000 5000  testfile-0664
-rwx------. 1 1000770000    0  testfile-0700
-rwxrwx---. 1 1000770000 5000  testfile-0770
drwxrwx---. 2 1000770000 5000  shared-dir/
-rw-rw----. 1 1000770000 5000  shared-dir/group-file
```

**Transfer status: Succeeded (100%)**

```
Status: Succeeded
Progress:
  Percentage: 100%
  Transferred: 1.01 K
  Files:
    Sent: 14
    Total: 7
```

**Target (after migration):**

| Property | Value |
|----------|-------|
| Pod UID | `1000810000` (target namespace UID) |
| Pod GID | `0` (root), supplemental groups `5000`, `1000810000` |
| SCC | `restricted-v2` |
| Transfer status | **Succeeded** |
| File read test | **ALL files readable** |
| File write test | **WRITE OK** |

```
uid=1000810000 gid=0(root) groups=0(root),5000,1000810000

-rw-r--r--. 1 1000810000 0  testfile-0644
-rw-rw----. 1 1000810000 0  testfile-0660     <- GID 5000 lost!
-rw-rw-r--. 1 1000810000 0  testfile-0664     <- GID 5000 lost!
-rwx------. 1 1000810000 0  testfile-0700
-rwxrwx---. 1 1000810000 0  testfile-0770     <- GID 5000 lost!
drwxrwx---. 2 1000810000 0  shared-dir/       <- GID 5000 lost!
-rw-rw----. 1 1000810000 0  shared-dir/group-file  <- GID 5000 lost!
```

| File | Source GID | Target GID | Mode | Transfer | Access impact |
|------|-----------|------------|------|----------|---------------|
| testfile-0644 | 0 | 0 | 0644 | OK | None (world-readable) |
| testfile-0660 | **5000** | **0** | 0660 | OK | **Shared access broken** |
| testfile-0664 | **5000** | **0** | 0664 | OK | Partially broken (world-readable) |
| testfile-0700 | 0 | 0 | 0700 | OK | None (owner-only) |
| testfile-0770 | **5000** | **0** | 0770 | OK | **Shared access broken** |
| shared-dir/ | **5000** | **0** | 0770 | OK | **Shared access broken** |
| shared-dir/group-file | **5000** | **0** | 0660 | OK | **Shared access broken** |

### Key findings

1. **Transfer succeeded 100%.** Unlike Scenario B, all files transferred successfully. This is because the rsync client UID (`1000770000`) matches the file **owner** UID -- both are the source namespace-allocated UID. The rsync client can read all files via owner permissions, regardless of group ownership.

2. **Shared GID (5000) is completely lost.** Every file that had `GID 5000` on the source now has `GID 0` on the target. Rsync runs without `--group`, so files are written with the rsync server's default GID (0, the root group).

3. **Permission mode bits are preserved.** The `0770`, `0660`, `0664` modes survive transfer unchanged. However, the group bits are now meaningless because the group changed from `5000` to `0`.

4. **Single-pod access works, but shared access is broken.** The target pod (which is the owner) can read and write all files. But a **second pod** that relies on `supplementalGroups: [5000]` for access would **fail** -- the files no longer have GID 5000, so the supplemental group is useless.

5. **No SCC issue.** Unlike Scenario B, this scenario worked under `restricted-v2` SCC. The `supplementalGroups` field uses `RunAsAny` strategy in restricted-v2, so GID 5000 was accepted without needing nonroot-v2.

### Interpretation

This scenario reveals a **silent data integrity issue**: the transfer succeeds cleanly (100%, no errors), but the shared-access semantics are quietly destroyed. The group ownership that enabled multi-pod access on the source is replaced with GID 0, breaking any workflow that depends on the shared GID pattern.

This is arguably **worse than a failure** because there is no error or warning to alert the user. The migration appears successful, but a secondary service trying to access the shared volume via `supplementalGroups: [5000]` will fail with permission denied.

**Potential mitigations:**
- Add `--rsync-supplemental-groups` flag to `transfer-pvc` to set supplemental groups on rsync pods, allowing rsync to preserve group ownership.
- Add `--preserve-groups` option that enables rsync `--group` flag (requires appropriate SCC).
- Post-migration hook or script to `chgrp` files back to the shared GID.
- Document this as a known limitation with a manual remediation step.

---

## How to run

### Prerequisites

- Two OpenShift clusters with NFS StorageClass (e.g. `managed-nfs-storage`)
- `oc`, `kubectl`, Go compiler
- `improved_user.sh` script for user/namespace creation
- Kubeadmin credentials for both clusters

### Running Scenario A

```bash
./scripts/migrate-pvcs-nfs-scenario-a.sh
```

### Running Scenario B

```bash
./scripts/migrate-pvcs-nfs-scenario-b.sh

# Override the explicit UID/GID (default: 1001)
EXPLICIT_UID=5432 EXPLICIT_GID=5432 ./scripts/migrate-pvcs-nfs-scenario-b.sh
```

### Running Scenario C

```bash
./scripts/migrate-pvcs-nfs-scenario-c.sh

# Override the shared GID (default: 5000)
SHARED_GID=6000 ./scripts/migrate-pvcs-nfs-scenario-c.sh
```

### Script configuration

All three scripts support the same environment variable overrides. Key differences:

| Variable | Scenario A | Scenario B | Scenario C |
|----------|-----------|-----------|-----------|
| `NAMESPACE` | `nfs-nonadmin-ns` | `nfs-scenario-b-ns` | `nfs-scenario-c-ns` |
| `USER_NAME` | `nfs-nonadmin-user` | `nfs-scenario-b-user` | `nfs-scenario-c-user` |
| `PVC_NAME` | `nfs-nonadmin-data` | `nfs-explicit-uid-data` | `nfs-shared-gid-data` |
| `EXPLICIT_UID` | _(N/A)_ | `1001` | _(N/A)_ |
| `EXPLICIT_GID` | _(N/A)_ | `1001` | _(N/A)_ |
| `SHARED_GID` | _(N/A)_ | _(N/A)_ | `5000` |

Common variables for all scripts:

| Variable | Default | Description |
|----------|---------|-------------|
| `NFS_STORAGE_CLASS` | `managed-nfs-storage` | NFS-backed StorageClass name |
| `ENDPOINT_TYPE` | `route` | Endpoint type for transfer-pvc |
| `WAIT_POD_TIMEOUT` | `300` | Max seconds to wait for pod readiness |

Other variables (`SOURCE_CLUSTER_URL`, `TARGET_CLUSTER_URL`, kubeadmin passwords, `OC_BIN`, `KUBECTL_BIN`, `GO_BIN`, `CRANE_BIN`, kubeconfig paths) are also overridable; see script headers.

---

## NFS-specific observations

### NFS provisioner cleanup

During testing, we observed that when an NFS PVC is deleted and a new PVC is created with the **same namespace and name**, the NFS subdir provisioner may not fully clean up the old directory before the new one is created. This can leave stale data on the NFS path. Use distinct namespace names or verify the NFS directory is clean when re-running tests.

### Block storage vs NFS: ownership behavior summary

| Aspect | Block storage (GCP PD, EBS) | NFS |
|--------|---------------------------|-----|
| `fsGroup` at mount time | Chowns all files to pod GID | Only adds supplemental group; no chown |
| Rsync without `--owner/--group` | Files owned by rsync server UID; then re-chowned by `fsGroup` at target mount | Files owned by rsync server UID; stays that way |
| Namespace UID mismatch after migration | `fsGroup` remaps at mount time | Rsync server UID = target namespace UID = target app UID (works correctly) |
| Explicit `runAsUser` mismatch | `fsGroup` may not help if UID is different | Rsync server UID differs from app UID (Scenario B) |
| Shared GID (`supplementalGroups`) | `fsGroup` may partially help | GID lost entirely; shared access broken (Scenario C) |

### Cross-scenario summary

| Scenario | UID pattern | Transfer result | Ownership after migration | Access after migration |
|----------|-------------|----------------|--------------------------|----------------------|
| **A** | Namespace-allocated (no explicit UID) | **Succeeded (100%)** | Remapped to target namespace UID | All files accessible |
| **B** | Explicit hardcoded UID (1001) | **Partially failed (50%)** | Mixed: some remapped, some original | Owner files OK; re-owned files need world-readable |
| **C** | Namespace UID + shared GID (5000) | **Succeeded (100%)** | UID remapped; **GID 5000 lost** (becomes 0) | Owner OK; **shared access broken** |

### Key takeaway

Crane `transfer-pvc` works correctly for NFS only when the workload uses **namespace-allocated UIDs** with no special group ownership (Scenario A). For workloads with explicit UIDs (Scenario B) or shared group access (Scenario C), the migration either partially fails or silently loses access semantics. These are real limitations that should be documented and addressed with additional flags or post-migration tooling.

---

## References

- **Issue #60:** [Migrate a volume containing data owned by non-admin group](https://github.com/konveyor-ecosystem/kubectl-migrate/issues/60)
- **Existing block-storage scenario:** [crane-migration-nonadmin-uid-scenario.md](crane-migration-nonadmin-uid-scenario.md)
- **Crane transfer-pvc:** Uses `getIDsForNamespace`, `getRsyncClientPodSecurityContext`, `getRsyncServerPodSecurityContext` in `cmd/transfer-pvc/transfer-pvc.go`
- **Script (Scenario A):** [scripts/migrate-pvcs-nfs-scenario-a.sh](../scripts/migrate-pvcs-nfs-scenario-a.sh)
- **Script (Scenario B):** [scripts/migrate-pvcs-nfs-scenario-b.sh](../scripts/migrate-pvcs-nfs-scenario-b.sh)
- **Script (Scenario C):** [scripts/migrate-pvcs-nfs-scenario-c.sh](../scripts/migrate-pvcs-nfs-scenario-c.sh)
