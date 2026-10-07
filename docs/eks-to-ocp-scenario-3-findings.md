# EKS-to-OCP Scenario 3: "Custom SCC MustRunAs" -- Findings

## Overview

This scenario creates a custom SCC (`scc-uid-1001`) with `MustRunAs` locked to
UID 1001, grants it to a dedicated service account, and deploys the target pod
with `runAsUser: 1001, runAsGroup: 1001, fsGroup: 1001`.

**Result: Partial success.** The pod runs as UID 1001 and can access files via group
membership. However, the 0700/0750 files were **never transferred** by rsync -- the
same data loss seen in Scenario 1. The custom SCC fixes the target identity but cannot
fix the upstream rsync transfer issue.

**Script:** `scripts/migrate-eks-to-ocp-scenario-3.sh`

## What's Different from Scenarios 1-2


| Aspect          | Scenario 1             | Scenario 2               | Scenario 3                       |
| --------------- | ---------------------- | ------------------------ | -------------------------------- |
| Target pod UID  | 1000740000 (namespace) | Pod never created        | **1001** (custom SCC)            |
| SCC             | restricted-v2          | restricted-v2 (rejected) | **scc-uid-1001** (custom)        |
| fsGroup         | None                   | 1001 (rejected)          | **1001** (allowed by custom SCC) |
| Service account | default                | default                  | **sa-uid-1001** (dedicated)      |


## Custom SCC Definition

```yaml
runAsUser:
  type: MustRunAs
  uid: 1001
fsGroup:
  type: MustRunAs
  ranges:
  - min: 1001
    max: 1001
supplementalGroups:
  type: MustRunAs
  ranges:
  - min: 1001
    max: 1001
```

`**MustRunAs` with `uid` field works correctly.** An earlier attempt used `MustRunAs`
with `uidRangeMin`/`uidRangeMax` (fields that belong to `MustRunAsRange`), which caused
SCC provider creation errors. The correct approach is `MustRunAs` + `uid: <value>` for
a single fixed UID, or `MustRunAsRange` + `uidRangeMin`/`uidRangeMax` for a range.

## Results

### Source (EKS, UID 1001)

```
uid=1001 gid=1001 groups=1001

-rw-r--r--. 1 1001 1001   testfile-0644
-rwx------. 1 1001 1001   testfile-0700
-rwxr-x---. 1 1001 1001   testfile-0750
-rwxr-xr-x. 1 1001 1001   testfile-0755
-rwxr-x---. 1 1001 1001   subdir/nested-file

All files: READ OK
```

### Target (OCP, custom SCC scc-uid-1001)

```
uid=1001(1001) gid=1001(1001) groups=1001(1001)
SCC: scc-uid-1001

-rw-rw-r--. 1 1000780000 1001   ownership-info
-rw-rw-r--. 1 1000780000 1001   testfile-0644
-rwxrwxr-x. 1 1000780000 1001   testfile-0755
                                  testfile-0700    -- MISSING (never transferred)
                                  testfile-0750    -- MISSING (never transferred)
                                  subdir/nested-file -- MISSING (never transferred)

testfile-0644: READ OK
testfile-0700: READ FAIL (file missing)
testfile-0750: READ FAIL (file missing)
testfile-0755: READ OK
subdir/nested-file: READ FAIL (file missing)
```

## Issues Found

### Issue 1: Restrictive files still lost during rsync transfer (same as Scenario 1)

**Severity:** Critical

The custom SCC fixes the target pod's UID/GID identity, but the 0700/0750 files
were never transferred by rsync in the first place. This is the same root cause
as Scenario 1: the rsync server pod on OCP runs as the namespace UID (1000780000),
and cannot read files owned by UID 1001 with owner-only or owner+group permissions.

**The custom SCC cannot fix this** because it only affects the target application pod,
not crane's rsync pods (which use the `default` SA and `restricted-v2` SCC).

### Issue 2: Transferred files are owned by the rsync server UID, not 1001

**Severity:** Medium

Files that were successfully transferred are owned by UID `1000780000` (the namespace
UID that the rsync server ran as), not by the original UID `1001`. The GID is `1001`
because of `fsGroup` chowning.

The target pod (running as UID 1001) accesses these files via **group membership**  
(GID 1001), not via UID ownership. This works for files with group-read permissions  
but is semantically different from the source ownership.

### Issue 4: `lost+found` directory not transferred (Permission denied)

**Severity:** Low

The rsync transfer reported `Partially failed` with:

```
Failed files:
  - /mnt/.../data/lost+found [Permission denied (13)]
```

The `lost+found` directory is created by `mkfs` on EBS EXT4 volumes with mode `drwxrws---`
owned by `root:root`. The rsync client cannot read it. This is generally harmless --
`lost+found` is a filesystem maintenance directory, not application data.

## Key Takeaway

The custom SCC approach solves the **target pod identity** problem (pod runs as UID 1001,
matching the source data), but **does not solve the rsync data transfer problem**. Files
with restrictive permissions (0700, 0750) are lost during the rsync phase regardless of
what SCC the target application pod uses.

To fully solve the problem, we would need either:

1. Make rsync run as UID 1001 (or root) to read all files -- requires modifying crane's
  `transfer-pvc` behavior
2. Use a privileged init-container to fix ownership after transfer -- tested in Scenario 4
3. Adjust source file permissions before migration (pre-migration step)

