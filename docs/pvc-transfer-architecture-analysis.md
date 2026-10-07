# PVC Transfer Architecture Analysis: Building a Better Migration Tool

## Problem Statement

Crane's `transfer-pvc` uses rsync to copy PVC data between clusters. Our EKS-to-OCP testing
across 4 scenarios proved this approach has fundamental limitations:

1. **Silent data loss:** Files with restrictive permissions (0700, 0750) are never transferred
   because the rsync client pod runs as a non-root UID that cannot read them
2. **UID/GID not preserved:** `restrictedContainers(true)` disables `--owner` and `--group`
   rsync flags (see `transfer-pvc.go` line 789-802)
3. **No incremental transfer:** Each run is a full copy
4. **No consistency guarantees:** rsync reads from a live volume while the app may be writing
5. **Direct connectivity required:** Source and target clusters need network path (via stunnel)

This document evaluates all viable approaches for PVC data transfer in a new migration tool
built on top of crane's manifest pipeline (export/transform) and integrated with ArgoCD.

---

## Root Cause: Why Crane's rsync Fails

```
cmd/transfer-pvc/transfer-pvc.go:

  restrictedContainers(true)  →  opts.Owners = false     (--no-owner)
                                  opts.Groups = false     (--no-group)
                                  opts.Permissions = false (--no-perms)
                                  opts.DeviceFiles = false
                                  opts.SpecialFiles = false

  getIDsForNamespace()  →  Reads openshift.io/sa.scc.uid-range annotation
                           Sets RunAsUser to namespace UID (e.g., 1000810000)
                           On EKS: no annotation → fallback UID
```

The rsync CLIENT pod mounts the source PVC and reads files. Because it runs as a non-root UID
(namespace-allocated on OCP, or fallback on EKS), it cannot read files with owner-only
permissions (0700, 0750). These files are silently skipped by rsync.

Even if rsync could read them, `--no-owner --no-group` means ownership metadata is lost.
The rsync SERVER creates files as its own UID on the destination.

---

## Options Evaluated

### Option 1: Fix Crane's rsync (run as root)

**Change:** Set `restrictedContainers(false)` and run rsync pods as root (UID 0).

**How it works:**
- Rsync client runs as root → can read ALL files regardless of permissions
- `--owner --group` flags preserved → ownership metadata transferred
- Rsync server runs as root → can set ownership on destination files

**Pros:**
- Minimal code change (flip a boolean + adjust SecurityContext)
- Keeps existing direct-connection architecture (stunnel)
- No new dependencies

**Cons:**
- Requires `privileged` or `anyuid` SCC on both clusters
- Source cluster (EKS) needs PSA exemption or privileged namespace
- Root rsync pods are a security concern -- cluster admins may reject this
- Still no incremental or consistency guarantees
- Still requires direct connectivity between clusters
- `--fake-super` alternative needs xattr filesystem support (not all CSI drivers support it)

**Verdict:** Quick fix but doesn't address the architectural limitations. Running privileged
pods is a hard sell in enterprise environments.

---

### Option 2: rsync with `--fake-super`

**Change:** Add `--fake-super` flag to rsync to store ownership in extended attributes.

**How it works:**
- Rsync stores UID/GID/permissions in xattrs (`user.rsync.%stat`) instead of setting them directly
- Works without root -- stores metadata alongside files
- A second pass (as root or init-container) reads xattrs and applies real ownership

**Pros:**
- No root requirement for the rsync transfer itself
- Preserves full ownership metadata
- Minimal architectural change

**Cons:**
- Requires filesystem xattr support on BOTH source and destination volumes
  (not all CSI drivers/filesystems support `user_xattr`)
- Two-phase process: transfer + ownership-restore
- Still doesn't solve the READ problem: rsync client still can't read 0700 files
  unless it runs as the file owner or root
- Complex error handling (xattr failures are subtle)

**Verdict:** Does NOT solve the core read-access problem. Even with `--fake-super`,
the rsync client needs to READ the source files first.

---

### Option 3: VolSync Operator (rsync-tls or Kopia method)

**Change:** Replace crane's `transfer-pvc` with VolSync CRDs.

**How it works:**
- Install VolSync operator on both clusters
- Create ReplicationSource (source) and ReplicationDestination (target)
- VolSync spawns mover pods that transfer data via rsync-tls, rclone, restic, or kopia
- Privileged mover annotation allows root access

**Pros:**
- Privileged movers run as root → solves the read-access problem
- Multiple transfer methods (rsync-tls, kopia, rclone, restic)
- Continuous replication on a schedule (not just one-shot)
- CRD-based → can be managed by ArgoCD (GitOps-friendly)
- Cross-cluster native
- CSI snapshot support for consistency
- Same project family as `pvc-transfer` library crane already uses (backube)

**Cons:**
- Requires operator installation on BOTH clusters (EKS needs Helm install)
- Operator lifecycle management (upgrades, compatibility)
- More moving parts (operator, CRDs, service accounts, RBAC)
- Community project (backube) -- not Red Hat supported
- For one-shot migration, an operator is heavyweight
- Namespace annotation required for privileged movers (cluster-admin needed)

**Verdict:** Best for ongoing replication and DR. Overkill for one-shot migration.
The operator dependency may be a barrier on managed K8s clusters where you can't install operators.

---

### Option 4: OADP/Velero Data Mover (CSI Snapshot + Kopia)

**Change:** Use Velero backup (source) + OADP restore (target) for PVC data only.

**How it works:**
- Source: Velero takes CSI snapshot, node-agent (Kopia) uploads to S3
- Target: OADP restores from S3 to a new PVC
- Node-agent runs as root with privileged SCC

**Pros:**
- Red Hat supported (OADP on OCP side)
- Node-agent runs as root → reads all files
- Kopia preserves UID/GID/permissions
- CSI snapshot provides point-in-time consistency
- Incremental/deduplicated via Kopia
- Direct path to CBT when CSI supports it

**Cons:**
- **Version coupling risk:** Upstream Velero on EKS may use different Kopia version than
  OADP's bundled Velero on OCP. Kopia repository format may be incompatible.
- OADP is officially for "same cluster" backup/restore (cross-cluster is a side effect)
- Velero uses a forked Kopia (`project-velero/kopia`), not upstream
- Heavy dependency stack: Velero + node-agent DaemonSet + CSI plugin + S3
- Imperative workflow (velero backup/restore commands) -- not GitOps-native
- Manifests are opaque blobs in Velero backups (we work around this with crane, but
  the PVC data is still behind Velero's API)
- Requires CSI snapshot support on source cluster

**Verdict:** Powerful but fragile for cross-platform (EKS-to-OCP) due to version coupling.
Best when both clusters run the same OADP version (OCP-to-OCP only).

---

### Option 5: Kopia Standalone (Embedded in the New Tool) ★ RECOMMENDED

**Change:** The new tool spawns Kopia pods (not rsync pods) to backup/restore PVC data
through S3 as an intermediary.

**How it works:**
```
Source cluster (EKS/K8s)                Target cluster (OCP)
┌────────────────────┐                  ┌────────────────────┐
│ Kopia backup pod   │                  │ Kopia restore pod  │
│ - runs as root     │     S3 bucket    │ - runs as root     │
│ - mounts source PVC│ ──────────────►  │ - mounts target PVC│
│ - kopia snapshot   │  (intermediary)  │ - kopia restore    │
│   create /data     │                  │   snapshot /data   │
└────────────────────┘                  └────────────────────┘
```

1. Tool creates a temporary S3-backed Kopia repository
2. Spawns a privileged pod on source cluster that mounts the PVC and runs
   `kopia snapshot create /data`
3. Spawns a privileged pod on target cluster that runs
   `kopia snapshot restore <id> /data`
4. Cleans up pods and optionally the S3 data

**Pros:**
- **Solves the read problem:** Pod runs as root → reads ALL files (0700, 0750, etc.)
- **Preserves everything:** Kopia stores full file metadata (UID, GID, permissions,
  xattrs, timestamps)
- **No operator dependency:** Just pods, like crane does today with rsync. Works on any
  K8s cluster without installing operators.
- **No version coupling:** The tool controls the exact Kopia version in the pod image.
  No Velero fork, no OADP compatibility matrix.
- **Incremental:** Subsequent runs only transfer changed files (Kopia deduplication)
- **Consistency:** Can take CSI snapshot first (if available), then backup from snapshot
- **S3 intermediary:** No direct cluster-to-cluster connectivity needed. Works across
  clouds, VPNs, air-gapped environments.
- **Same engine as OADP:** Kopia is what OADP Data Mover uses internally. When CBT
  matures, the same Kopia repository format will work.
- **Embeddable:** Kopia is a Go library -- can be compiled into the tool binary, or
  used as a container image for the pods.
- **Encryption:** Kopia encrypts data at rest in the repository by default.

**Cons:**
- Requires S3 bucket (or compatible object storage) as intermediary
- Two-step process (backup + restore) instead of direct stream
- Slightly higher latency than direct rsync (data goes through S3)
- Need to build and maintain a Kopia pod image
- Privileged pods still needed (same as all solutions that preserve ownership)
- S3 costs for storing the intermediate data

**Verdict:** Best balance of correctness, portability, and simplicity. No operator
dependencies, no version coupling, works on any K8s cluster, and uses the same engine
that the broader ecosystem (OADP, VolSync) is converging on.

---

### Option 6: pv-migrate

**Change:** Use the pv-migrate tool instead of crane's transfer-pvc.

**How it works:**
- CLI tool that uses rsync+SSH or rsync via LoadBalancer Service
- Mounts PVCs and runs rsync between them

**Pros:**
- Simple CLI tool, no operator dependency
- Multiple strategies (mnt2, svc, lbsvc)
- Actively maintained

**Cons:**
- Still rsync-based → same permission problems as crane
- No privileged mode → same data loss for 0700/0750 files
- No incremental or deduplication
- No ownership preservation in restricted mode
- Doesn't solve any of our core issues

**Verdict:** Same fundamental limitation as crane's rsync. Not a solution.

---

### Option 7: rclone (via S3 intermediary)

**Change:** Use rclone to sync PVC data through S3.

**How it works:**
- Source pod: rclone sync /data s3://bucket/path
- Target pod: rclone sync s3://bucket/path /data

**Pros:**
- S3 intermediary (no direct connectivity)
- Many backend support (S3, GCS, Azure, etc.)
- Well-maintained tool

**Cons:**
- **Does NOT preserve Unix permissions/UID/GID over S3:** S3 is an object store with
  no concept of Unix file ownership. Metadata is lost.
- Running as root helps READ files but ownership is still lost in transit
- No incremental (rclone sync is efficient but not deduplicated)
- No encryption at rest (unless S3 bucket encryption is configured)

**Verdict:** Cannot preserve file ownership through S3. Fundamentally unsuitable for
our use case where UID/GID preservation matters.

---

## Comparison Matrix

| Criteria | Crane rsync | rsync --fake-super | VolSync | OADP/Velero | Kopia Standalone | pv-migrate | rclone |
|----------|------------|-------------------|---------|-------------|-----------------|------------|--------|
| Reads 0700/0750 files | NO | NO | YES | YES | YES | NO | YES (as root) |
| Preserves UID/GID | NO | Partial (xattr) | YES | YES | YES | NO | NO |
| No operator needed | YES | YES | NO | NO | YES | YES | YES |
| No version coupling | YES | YES | YES | NO | YES | YES | YES |
| Incremental | NO | NO | YES | YES | YES | NO | Partial |
| Consistency (snapshot) | NO | NO | YES | YES | Optional | NO | NO |
| Cross-cloud (no direct conn) | NO | NO | Optional | YES | YES | NO | YES |
| GitOps-friendly | NO | NO | YES | Partial | YES (stateless) | NO | NO |
| Red Hat supported | NO | NO | NO | YES | NO | NO | NO |
| Complexity | Low | Medium | High | High | Medium | Low | Medium |

---

## Recommendation: Kopia Standalone as the Primary Transfer Engine

### Why Kopia Standalone is the best fit

1. **It solves the exact problem we proved exists.** Our 4 EKS-to-OCP scenarios demonstrated
   that the rsync client cannot read restrictive files. Kopia pods running as root fix this.

2. **No operator tax.** Unlike VolSync or OADP, there's nothing to install on either cluster
   except the pods the tool spawns (same model as crane today). This works on managed EKS,
   GKE, AKS, self-managed K8s, and OCP -- anywhere you can run a pod.

3. **No version coupling.** The tool controls the Kopia version in its pod image. No
   dependency on OADP's Velero fork or VolSync's operator version.

4. **Same engine the ecosystem is converging on.** OADP Data Mover uses Kopia. VolSync
   supports Kopia. When CBT matures, Kopia repositories will be the standard format.
   Building on Kopia now means the tool's data format is forward-compatible.

5. **S3 as intermediary is a feature, not a bug.** It decouples the clusters -- no stunnel,
   no direct connectivity, works across clouds and air-gapped environments. The data is
   encrypted at rest in the Kopia repository.

6. **Incremental for free.** After the initial migration, subsequent runs only transfer
   changed files. This enables the "scheduled sync" pattern for DR without needing an
   operator.

### Proposed Architecture for the New Tool

```
┌─────────────────────────────────────────────────────────────────────┐
│                        NEW MIGRATION TOOL                          │
│                                                                     │
│  ┌─────────────┐    ┌─────────────┐    ┌──────────────────┐        │
│  │ crane export │ →  │ crane       │ →  │ Git push         │        │
│  │              │    │ transform   │    │ (manifests)      │        │
│  └─────────────┘    └─────────────┘    └──────────────────┘        │
│                                               │                     │
│                                               ▼                     │
│                                        ┌──────────────┐             │
│                                        │ ArgoCD sync  │             │
│                                        │ (target)     │             │
│                                        └──────────────┘             │
│                                                                     │
│  ┌──────────────────────────────────────────────────────────┐       │
│  │              PVC Transfer (Kopia-based)                  │       │
│  │                                                          │       │
│  │  Source cluster          S3 bucket       Target cluster  │       │
│  │  ┌────────────┐      ┌───────────┐    ┌──────────────┐  │       │
│  │  │ Kopia pod  │ ───► │ Kopia     │ ─► │ Kopia pod    │  │       │
│  │  │ (backup)   │      │ repository│    │ (restore)    │  │       │
│  │  │ root, PVC  │      │ encrypted │    │ root, PVC    │  │       │
│  │  └────────────┘      └───────────┘    └──────────────┘  │       │
│  └──────────────────────────────────────────────────────────┘       │
│                                                                     │
│  CLI:  newtool migrate --source-kubeconfig=... \                   │
│                         --target-kubeconfig=... \                   │
│                         --pvc-name=mydata \                         │
│                         --s3-bucket=migration-repo \                │
│                         --git-repo=github.com/org/app-manifests    │
└─────────────────────────────────────────────────────────────────────┘
```

### Implementation Approach

**Phase 1 -- Manifest pipeline (already working):**
- Crane export + transform + push to Git
- ArgoCD deploys on target

**Phase 2 -- Kopia-based PVC transfer:**
- Build a container image with Kopia CLI (or embed Kopia Go library)
- Tool creates a Kopia repository in S3 (via `kopia repository create s3`)
- Tool spawns a privileged backup pod on source cluster:
  - Mounts the source PVC
  - Runs `kopia snapshot create /data`
  - Pod is cleaned up after completion
- Tool spawns a privileged restore pod on target cluster:
  - Mounts the target PVC (created by the tool or by ArgoCD)
  - Runs `kopia snapshot restore <snapshot-id> /data`
  - Pod is cleaned up after completion
- S3 data retained for incremental subsequent runs or cleaned up

**Phase 3 -- Optional VolSync integration for continuous DR:**
- For users who want ongoing replication (not just one-shot migration)
- Tool generates VolSync CRDs and commits them to Git
- ArgoCD deploys VolSync resources alongside the app
- This is additive -- doesn't replace the Kopia-based transfer

### Why Not VolSync as Primary?

VolSync is excellent but has a higher barrier to entry:
- Operator must be installed on both clusters (cluster-admin action)
- On managed K8s (EKS, GKE), installing operators may require admin approval
- For a one-shot migration, installing an operator is overhead

The Kopia standalone approach works like crane does today: spawn pods, do the work, clean up.
No residual operator running after migration is complete.

VolSync becomes valuable AFTER migration, for ongoing DR replication. The tool can support
both: Kopia for migration, VolSync CRDs for continuous sync.

### Why Not OADP as Primary?

OADP is the most "enterprise" option but has critical limitations for K8s-to-OCP:
- OADP is an OCP operator -- cannot install on vanilla K8s/EKS
- Upstream Velero on EKS + OADP on OCP = version coupling risk
- Velero's forked Kopia adds compatibility uncertainty
- OADP's primary design is same-cluster backup/restore
- Imperative workflow (velero backup/restore) conflicts with GitOps goals

### Security Considerations

All solutions that preserve UID/GID require privileged pods. This is unavoidable because:
- Reading files as root requires `runAsUser: 0`
- Setting ownership on restore requires root or CAP_CHOWN
- On OCP: requires `anyuid` or `privileged` SCC on the service account
- On EKS: requires PSA exemption or privileged namespace

The tool should:
1. Create a dedicated service account for transfer pods
2. Grant minimum required SCC/PSA (anyuid is sufficient, not full privileged)
3. Clean up the SCC grant after transfer completes
4. Document the security requirements clearly

### S3 Cost Mitigation

- Kopia repository can use lifecycle policies to auto-delete after N days
- For one-shot migration: delete the S3 data after successful restore
- For incremental/DR: retain with Kopia's built-in snapshot retention policies
- Kopia deduplication minimizes storage usage for incremental runs
