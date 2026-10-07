# EKS-to-OCP Scenario 2: "The fsGroup Fix" -- Findings

## Overview

This scenario tests whether setting `fsGroup: 1001` in the target pod's `securityContext`
can fix file access after migrating an EBS-backed PVC from EKS (UID 1001) to OCP on AWS.
The idea is that `fsGroup` tells the kubelet to chown the volume's GID at mount time,
allowing the pod to access files via group membership.

**Result: The `restricted-v2` SCC rejected the pod outright.** `fsGroup: 1001` is not
in the namespace's allowed supplemental-groups range, so the pod was never created.

**Script:** `scripts/migrate-eks-to-ocp-scenario-2.sh`

## Environment

| Property | EKS (Source) | OCP (Target) |
|---|---|---|
| StorageClass | `gp3` (ebs.csi.aws.com) | `gp3-csi` (ebs.csi.aws.com) |
| Namespace UID range | None | `1000760000/10000` |
| Supplemental groups range | None | `1000760000/10000` |
| SCC | N/A (PSA only) | `restricted-v2` |
| App UID | 1001 | Pod never created |

## What Happened

### Steps 1-9: Identical to Scenario 1

- Source app deployed on EKS as UID 1001 with files at 0644/0700/0750/0755 permissions
- Crane export, transform, transfer-pvc all succeeded
- Transfer-pvc reported "Partially failed" (only `lost+found` permission denied -- expected)

### Step 10: Apply Deployment with fsGroup: 1001

The target Deployment was applied with this pod-level `securityContext`:

```yaml
securityContext:
  fsGroup: 1001
```

The `oc apply` itself succeeded with a PodSecurity warning:

```
Warning: would violate PodSecurity "restricted:latest": runAsNonRoot != true
```

But the ReplicaSet **could not create any pod**:

```
Error creating: pods "eks-ocp-scenario-2-ffb7945dd-" is forbidden:
unable to validate against any security context constraint:
  provider restricted-v2: .spec.securityContext.fsGroup:
    Invalid value: []int64{1001}: 1001 is not an allowed group
```

## Issue Found

### Issue 1: `restricted-v2` SCC rejects fsGroup values outside the namespace range

**Severity:** Blocking (pod never starts)

The `restricted-v2` SCC uses `MustRunAs` for its `fsGroup` policy. This means the only
allowed fsGroup values are those within the namespace's `openshift.io/sa.scc.supplemental-groups`
annotation range. For this namespace, that range is `1000760000/10000` (i.e., GIDs
1000760000 through 1000769999).

Setting `fsGroup: 1001` is rejected because 1001 falls outside this range.

**Why this matters for EKS-to-OCP migration:**

On EKS, applications commonly run as well-known non-root UIDs (e.g., 1001 for nginx,
999 for PostgreSQL, 1000 for custom apps). These UIDs are chosen by the container image
or the Deployment author. There is no namespace-level UID allocation on EKS.

On OCP, the `restricted-v2` SCC enforces that:
- `runAsUser` must be within the namespace UID range
- `fsGroup` must be within the namespace supplemental-groups range
- Both ranges are auto-allocated by OpenShift (e.g., `1000760000/10000`)

This creates a fundamental incompatibility: you cannot simply set `fsGroup` to the
source UID/GID to "fix" ownership, because the SCC will reject it.

### Issue 2: `lost+found` directory not transferred (Permission denied)

**Severity:** Low

The rsync transfer reported `Partially failed` with:

```
Failed files:
  - /mnt/.../data/lost+found [Permission denied (13)]
```

The `lost+found` directory is created by `mkfs` on EBS EXT4 volumes with mode `drwxrws---`
owned by `root:root`. The rsync client cannot read it. This is generally harmless --
`lost+found` is a filesystem maintenance directory, not application data.

### Possible workaround (not tested in this scenario)

To use `fsGroup: 1001`, you would need to either:
1. Grant the service account a more permissive SCC (e.g., `nonroot-v2` with `RunAsAny`
   for fsGroup) -- tested in Scenario 3
2. Use an init-container with elevated privileges to `chown` the data -- tested in Scenario 4
3. Create a custom SCC that allows fsGroup: 1001 specifically

## Key Takeaway

The `fsGroup` fix does **not** work on OCP with default `restricted-v2` SCC when the
source GID is outside the namespace's allocated range. This is a second layer of defense
in OCP's security model that blocks the most intuitive remediation approach.

## Comparison with Scenario 1

| Aspect | Scenario 1 (Strict Move) | Scenario 2 (fsGroup Fix) |
|---|---|---|
| Transfer | OK (but 0700/0750 files lost) | OK (same behavior) |
| Pod creation | OK (namespace UID assigned) | **BLOCKED by SCC** |
| File access | Partial (only 0644/0755) | N/A (pod never started) |
| Root cause | UID mismatch | SCC rejects fsGroup: 1001 |

Scenario 2 is actually a **worse** outcome than Scenario 1 -- at least in Scenario 1
the pod started and could access some files. With `fsGroup: 1001`, the pod is completely
blocked from starting.
