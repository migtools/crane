# Debug: MTA-912 Download Pod Timeout

## Error Summary
Jenkins job 163 fails on MTA-912 indirect transfer test (MySQL):
- **Error**: `download pod failed: timed out waiting for pod indirect-rclone-secret-mysql/rclone-download-mysql-data to start: context deadline exceeded`
- **Phase**: [4/6] Downloading data from cloud storage
- **Pod**: `rclone-download-mysql-data` in namespace `indirect-rclone-secret-mysql`
- **Timeout**: 5 minutes (from `followPodLogsUntilComplete` line 276)

## Root Cause Analysis

Based on prior MTA-912 investigation (memory: mta912_awsgcp_rerun.md), the timeout happens when:

1. **Destination PVC stays in Pending state** because:
   - Storage class name mismatch (source PVC class doesn't exist on destination cluster)
   - OR storage class not specified and default doesn't match

2. **Download pod can't start** because:
   - Pod spec mounts the Pending PVC
   - kubelet keeps pod in `Pending` or `ContainerCreating` state
   - Pod never reaches `Running` / `Succeeded` / `Failed` 
   - 5-minute wait times out

3. **Error is unhelpful** because:
   - Error message doesn't show PVC binding status
   - Pod is deleted by deferred cleanup before investigation
   - No pod events or describe output captured

## Code Locations

- **Timeout wait**: `cmd/transfer-pvc/indirect.go:276` in `followPodLogsUntilComplete()`
  - `wait.PollUntilContextCancel(startCtx, 3*time.Second, true, ...)`
  - Context timeout: 5 minutes

- **Download pod creation**: `github.com/konveyor/crane-lib/state_transfer/transfer/indirect`
  - Pod created with PVC mount referencing `destPVC.Name`

- **Destination PVC creation**: `cmd/transfer-pvc/indirect.go:143-149`
  - Calls `buildDestinationPVC()` which copies source spec + storage class
  - Calls `createDestinationPVC()` which may reuse existing PVC or create new one

## Potential Causes for Recent Failure

### Hypothesis 1: Storage Class Mismatch (Most Likely)
- Test environment changed storage class availability
- New crane-lib version changes how storage class is determined
- Test doesn't specify `--dest-storage-class` explicitly
- Source PVC has non-default storage class that doesn't exist on dest

### Hypothesis 2: PVC Binding Delay
- Cluster has limited storage resources
- Volume provisioning is slow (5+ minutes)
- Dynamic provisioner is delayed

### Hypothesis 3: Image Pull Timeout
- Download pod can't pull image within timeout
- Network or registry issues
- But this would be "ImagePullBackOff", not "Pending"

### Hypothesis 4: Node Capacity/Scheduling
- No suitable nodes to schedule pod
- Taints/tolerations mismatch
- Resource requests too high

## Investigation Steps

1. **Check test logs from Jenkins 163** for pod events/describe
2. **Verify PVC status** at time of failure - is it Pending or Bound?
3. **Check storage classes** on test cluster - what classes are available?
4. **Look for recent cluster changes** - new storage backend, provisioner issues
5. **Compare with working run** - environment configuration differences

## Quick Fixes to Try

1. **Increase timeout** in `followPodLogsUntilComplete()` from 5 to 10 minutes
2. **Add diagnostic logging** before timeout:
   ```go
   pod := &corev1.Pod{}
   c.Get(ctx, client.ObjectKey{...}, pod)
   // Log pod phase, conditions, events
   ```
3. **Check and report PVC status** when pod startup fails
4. **Make test specify `--dest-storage-class`** explicitly to rule out mismatch
5. **Add pre-flight validation** of destination storage class existence

## Related Issues
- MTA-912 AWS→GCP rerun failed for same reason (storage class mismatch)
- Fix confirmed with `--dest-storage-class standard-csi`
- Product gaps identified: no pre-flight storage class validation, unhelpful timeout errors
