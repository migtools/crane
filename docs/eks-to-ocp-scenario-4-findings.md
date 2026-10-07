# EKS-to-OCP Scenario 4: "Migration Hook (Init-Container)" -- Findings

## Overview

This scenario uses a privileged init-container (running as root via `anyuid` SCC) to
`chown -R` all migrated data to UID/GID 1001 (the original source UID) before the main
container starts. The main container then runs as UID 1001 -- identical to the source.

**Result: Partial success (same data loss).** The init-container successfully chowned all
transferred files from the namespace UID (`1000820000`) to `1001:1001`. The main container
runs as UID 1001 and owns all transferred files. However, `testfile-0700`, `testfile-0750`,
and `subdir/nested-file` were **never transferred** by rsync -- even root cannot see them
on the target volume because they don't exist.

**Script:** `scripts/migrate-eks-to-ocp-scenario-4.sh`

## What's Different from Scenarios 1-3


| Aspect         | Scenario 1    | Scenario 2               | Scenario 3                | Scenario 4               |
| -------------- | ------------- | ------------------------ | ------------------------- | ------------------------ |
| Target pod UID | Namespace UID | Pod never created        | 1001 (custom SCC)         | **1001 (anyuid SCC)**    |
| SCC            | restricted-v2 | restricted-v2 (rejected) | scc-uid-1001 (custom)     | **anyuid**               |
| Init-container | None          | None                     | None                      | **Root (chown to 1001)** |
| File ownership | Namespace UID | N/A                      | Namespace UID (via group) | **1001:1001 (chowned)**  |


## Hook Configuration

### Service account and SCC grant

```bash
oc create sa sa-hook -n <namespace>
oc adm policy add-scc-to-user anyuid -z sa-hook -n <namespace>
```

### Deployment with init-container

```yaml
spec:
  serviceAccountName: sa-hook
  initContainers:
  - name: fix-ownership
    image: registry.access.redhat.com/ubi8/ubi-minimal:latest
    command: ["/bin/sh", "-c"]
    args:
    - |
      chown -R 1001:1001 /data/
      chmod -R u+rwX,g+rX /data/
    securityContext:
      runAsUser: 0
    volumeMounts:
    - name: data
      mountPath: /data
  containers:
  - name: app
    securityContext:
      runAsUser: 1001
      runAsGroup: 1001
```

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

### Init-container (root) -- Files BEFORE chown

```
uid=0(root) gid=0(root) groups=0(root)

-rw-r--r--. 1 1000820000 1000820000   ownership-info
-rw-r--r--. 1 1000820000 1000820000   testfile-0644
-rwxr-xr-x. 1 1000820000 1000820000   testfile-0755

testfile-0700    -- NOT PRESENT (never transferred)
testfile-0750    -- NOT PRESENT (never transferred)
subdir/nested-file -- NOT PRESENT (never transferred)
```

This is the definitive proof: **even root cannot see these files because they don't exist
on the target volume.** The rsync transfer never sent them.

### Init-container -- Files AFTER chown

```
-rw-r--r--. 1 1001 1001   ownership-info
-rw-r--r--. 1 1001 1001   testfile-0644
-rwxr-xr-x. 1 1001 1001   testfile-0755
```

All transferred files are now owned by `1001:1001`, matching the source.

### Target (OCP, UID 1001, after init-container chown)

```
uid=1001(1001) gid=1001(1001) groups=1001(1001)
SCC: anyuid

-rw-r--r--. 1 1001 1001   ownership-info
-rw-r--r--. 1 1001 1001   testfile-0644
-rwxr-xr-x. 1 1001 1001   testfile-0755

testfile-0644: READ OK
testfile-0700: READ FAIL (file missing)
testfile-0750: READ FAIL (file missing)
testfile-0755: READ OK
subdir/nested-file: READ FAIL (file missing)
```

## Issues Found

### Issue 1: Restrictive files never transferred by rsync (confirmed by root)

**Severity:** Critical

The init-container running as root confirmed that `testfile-0700`, `testfile-0750`, and
`subdir/nested-file` do not exist on the target volume at all. This proves the data loss
occurs during the rsync transfer phase, not due to target-side permissions.

**The init-container approach cannot solve this problem** -- it can only fix ownership
and permissions of files that were successfully transferred.

### Issue 2: `anyuid` SCC does not allow seccomp profiles

**Severity:** Medium (operational)

The initial attempt included `seccompProfile: type: RuntimeDefault` on the main container,
which caused the `anyuid` SCC to reject the pod:

```
pod.metadata.annotations[container.seccomp.security.alpha.kubernetes.io/app]:
Forbidden: seccomp may not be set
```

**Fix:** Remove `seccompProfile` from the container security context when using `anyuid`.

### Issue 3: `lost+found` directory not transferred (Permission denied)

**Severity:** Low

The rsync transfer reported `Partially failed` with:

```
Failed files:
  - /mnt/.../data/lost+found [Permission denied (13)]
```

The `lost+found` directory is created by `mkfs` on EBS EXT4 volumes with mode `drwxrws---`
owned by `root:root`. The rsync client cannot read it. This is generally harmless --
`lost+found` is a filesystem maintenance directory, not application data.

Note: the init-container (running as root) was able to see and chown `lost+found`
successfully, but the directory was already empty on the target since rsync couldn't
transfer its contents.

### Issue 4: Init-container chown provides real value here (unlike with NS UID)

**Severity:** Informational

 Chowning to `1001:1001` provides
real value:

- **Before chown:** Files owned by `1000820000:1000820000` (rsync server's namespace UID)
- **After chown:** Files owned by `1001:1001` (matching original source ownership)
- The main container (UID 1001) now owns the files directly, not via group membership

This is the cleanest ownership match of any scenario -- transferred files have identical
UID:GID ownership to the source.

## Key Takeaway

The init-container migration hook provides **the best possible target-side result**: files
are owned by the original UID (1001) and the main container runs as that UID. However, it
still cannot recover the 3 files that rsync never transferred.

**The root cause is confirmed:** crane's rsync client cannot read source files with
restrictive permissions (0700, 0750) when the rsync client runs as a different UID than the
file owner. No target-side remediation can fix this.

## Cross-Scenario Summary


| Scenario          | Target SCC               | Target UID    | Ownership Match  | Files Transferred | Data Loss        |
| ----------------- | ------------------------ | ------------- | ---------------- | ----------------- | ---------------- |
| 1: Strict Move    | restricted-v2            | Namespace UID | No               | 2/5               | 3 files lost     |
| 2: fsGroup Fix    | restricted-v2 (rejected) | N/A           | N/A              | N/A               | Pod blocked      |
| 3: Custom SCC     | scc-uid-1001             | 1001          | Partial (group)  | 2/5               | 3 files lost     |
| 4: Migration Hook | anyuid                   | **1001**      | **Yes (direct)** | 2/5               | **3 files lost** |


Scenario 4 achieves the best ownership match but the same data loss. The problem is
upstream in rsync, not downstream in the target pod configuration.