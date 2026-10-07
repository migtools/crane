# EKS-to-OCP Scenario 1: "The Strict Move" -- Findings

## Overview

This scenario tests a direct ("strict") migration of an EBS-backed PVC from EKS to OCP
using crane, where the source application runs as UID 1001. On the OCP target, the default
`restricted-v2` SCC assigns a namespace-allocated UID (1000740000), resulting in file access
failures for owner-restricted files.

**Script:** `scripts/migrate-eks-to-ocp-scenario-1.sh`

## Environment


| Property                  | EKS (Source)                                | OCP (Target)                    |
| ------------------------- | ------------------------------------------- | -------------------------------- |
| StorageClass              | `gp3` (ebs.csi.aws.com)                     | `gp3-csi` (ebs.csi.aws.com)      |
| Namespace UID range       | None (EKS has no OpenShift UID annotations) | `1000740000/10000`               |
| Supplemental groups range | None                                        | `1000740000/10000`               |
| SCC                       | N/A (PSA only)                              | `restricted-v2`                  |
| App UID                   | 1001                                        | 1000740000 (namespace-allocated) |


## Test Files Created on EKS


| File                 | Permissions  | Owner     | Purpose                     |
| -------------------- | ------------ | --------- | --------------------------- |
| `testfile-0644`      | `-rw-r--r--` | 1001:1001 | World-readable              |
| `testfile-0700`      | `-rwx------` | 1001:1001 | Owner-only (private)        |
| `testfile-0750`      | `-rwxr-x---` | 1001:1001 | Owner + group readable      |
| `testfile-0755`      | `-rwxr-xr-x` | 1001:1001 | World-readable + executable |
| `subdir/nested-file` | `-rwxr-x---` | 1001:1001 | Nested, group-readable      |


## Results

### Source (EKS)

```
uid=1001 gid=1001 groups=1001

-rw-r--r--. 1 1001 1001   testfile-0644
-rwx------. 1 1001 1001   testfile-0700
-rwxr-x---. 1 1001 1001   testfile-0750
-rwxr-xr-x. 1 1001 1001   testfile-0755
-rwxr-x---. 1 1001 1001   subdir/nested-file

All files: READ OK
```

### Target (OCP)

```
uid=1000740000 gid=0(root) groups=0(root),1000740000
SCC: restricted-v2

-rw-rw-r--. 1 1000740000 1000740000   testfile-0644
                                        testfile-0700    -- MISSING
                                        testfile-0750    -- MISSING
-rwxrwxr-x. 1 1000740000 1000740000   testfile-0755
                                        subdir/nested-file -- MISSING

testfile-0644: READ OK
testfile-0700: READ FAIL (file missing)
testfile-0750: READ FAIL (file missing)
testfile-0755: READ OK
subdir/nested-file: READ FAIL (file missing)
```

## Issues Found

### Issue 1: Owner-only and group-restricted files are NOT transferred by rsync

**Severity:** Critical

The files with permissions `0700` and `0750` were not just inaccessible on the target -- they
were **completely missing** from the target volume. The rsync transfer itself dropped them.

**Why this happens:**

The `transfer-pvc` command creates an rsync server pod on OCP (target) that runs as the
namespace-allocated UID (1000740000), as dictated by `getIDsForNamespace()` in
`cmd/transfer-pvc/transfer-pvc.go`. On EKS (source), the rsync client pod has no explicit
UID set (EKS has no `openshift.io/sa.scc.uid-range` annotations), so it runs as whatever
the container image default is.

When rsync copies files from the source PVC, it encounters:

- `testfile-0700` (owner-only: `1001:1001`, mode `-rwx------`)
- `testfile-0750` (owner+group: `1001:1001`, mode `-rwxr-x---`)
- `subdir/nested-file` (group-readable: `1001:1001`, mode `-rwxr-x---`)

Since the rsync client on EKS does not run as UID 1001 (it runs as root or a different UID),
**these files may not have been readable by the rsync process itself**, causing them to be
silently skipped. The rsync transfer reported "Partially failed" with the `lost+found`
permission denial, but the 0700/0750 files were silently not transferred.

**Impact:** Data loss during migration. Files with restrictive permissions are silently
dropped, with no clear error message indicating which application files were skipped.

### Issue 2: Ownership is remapped to namespace UID by the EBS CSI fsGroup mechanism

**Severity:** Medium

Even for files that were successfully transferred (0644 and 0755), their ownership was
changed from `1001:1001` to `1000740000:1000740000`. This happens because:

1. The rsync server pod on OCP runs as UID 1000740000
2. The EBS CSI driver applies `fsGroup` (from the namespace's supplemental-groups annotation)
  at volume mount time
3. Files are recursively chowned to match the namespace-allocated GID

While this doesn't break access for the specific target pod, it means the data no longer
matches the original application's expected UID/GID. Applications that store UID-sensitive
metadata (e.g., PostgreSQL data directories, SSH keys) may behave incorrectly.

### Issue 3: Permission bits are also changed

**Severity:** Low

Transferred files had their permission bits altered:

- `testfile-0644` changed from `-rw-r--r--` to `-rw-rw-r--` (group-write added)
- `testfile-0755` changed from `-rwxr-xr-x` to `-rwxrwxr-x` (group-write+exec added)

This is a side effect of the fsGroup mechanism adding group permissions. While usually
harmless, it changes the security posture of the files.

### Issue 4: `lost+found` directory not transferred (Permission denied)

**Severity:** Low

The rsync transfer reported `Partially failed` with:

```
Failed files:
  - /mnt/.../data/lost+found [Permission denied (13)]
```

The `lost+found` directory is created by `mkfs` on EBS EXT4 volumes with mode `drwxrws---`
owned by `root:root`. The rsync client cannot read it since it doesn't run as root. This is
generally harmless -- `lost+found` is a filesystem maintenance directory, not application data.

### Issue 5: Exported Deployment contains EKS-specific securityContext that is incompatible with OCP

**Severity:** Medium

The crane-exported Deployment YAML (see `output-eks-ocp-s1/resources/`) retains the
original EKS `securityContext`:

```yaml
securityContext:
  fsGroup: 1001
  runAsGroup: 1001
  runAsUser: 1001
```

If applied directly to OCP, the `restricted-v2` SCC would **reject** the pod because
UID 1001 is not in the namespace's allowed UID range (`1000740000/10000`). The SCC uses
`MustRunAsRange` for `runAsUser`, which requires the UID to fall within the namespace
annotation range.

**Workaround used:** The script strips the `securityContext` from the Deployment and
deploys with no explicit UID, letting `restricted-v2` assign the namespace UID.

## Key Takeaway

A "strict move" of EBS-backed data from EKS to OCP fails on multiple levels:

1. **Data loss:** Files with restrictive permissions (0700, 0750) are silently not transferred
2. **UID mismatch:** The target pod runs as a completely different UID
3. **Manifest incompatibility:** The exported Deployment's securityContext is rejected by OCP's SCC

