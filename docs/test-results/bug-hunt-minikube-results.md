# Bug Hunt Test Results — Minikube

Date: 2026-08-31
Environment: Minikube dual-cluster (src/tgt contexts)

## Summary

- **Tests run:** 11
- **Bugs found:** 2 (PVC-D11, PVC-D16)
- **Needs real cluster:** 1 (SC-10)
- **Needs team discussion:** 1 (EXP-08)
- **Pass:** 6
- **Automated as e2e:** 1 (MTA-901)

## Results

### PVC-D11: Destination PVC AlreadyExists skips StorageClass validation

**Status:** Bug found

**Commands:**
```bash
# Pre-create destination PVC on wrong StorageClass
kubectl create -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: mongodb-data-new
  namespace: tgt-ns
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: standard
  resources:
    requests:
      storage: 1Gi
EOF

# Transfer with a different --dest-storage-class
crane transfer-pvc \
  --source-context src --destination-context tgt \
  --pvc-name mongodb-data:mongodb-data-new \
  --pvc-namespace src-ns:tgt-ns \
  --dest-storage-class crane-dest-sc \
  --endpoint nginx-ingress --subdomain <node-ip>.nip.io
```

**Result:** Crane sees AlreadyExists on dest PVC, skips creation, proceeds with transfer. Data lands on `standard` SC instead of `crane-dest-sc` — no error or warning.

---

### PVC-D16: rsync exit 23 reported as "succeeded"

**Status:** Bug found (fix applied locally)

**Commands:**
```bash
# Deploy MongoDB (creates files with mixed ownership including root-owned .mongodb)
kubectl apply -f e2e-tests/apps/mongodb/deployment.yaml -n src-ns

# Transfer PVC
crane transfer-pvc \
  --source-context src --destination-context tgt \
  --pvc-name mongodb-data:mongodb-data-new \
  --pvc-namespace src-ns:tgt-ns \
  --endpoint nginx-ingress --subdomain <node-ip>.nip.io
```

**Result:** rsync exits with code 23 (partial transfer — `.mongodb` file permission denied). Crane reports "succeeded" with no warning.

---

### SC-05: SC conversion — PVC rename + pvc-rename-map

**Status:** Pass

**Commands:**
```bash
# Scale down workload
kubectl scale deployment/mongodb --replicas=0 -n src-ns

# Transfer PVC with rename and different StorageClass
crane transfer-pvc \
  --source-context src --destination-context src \
  --pvc-name mongodb-data:mongodb-data-new \
  --pvc-namespace src-ns \
  --dest-storage-class crane-dest-sc \
  --endpoint nginx-ingress --subdomain <node-ip>.nip.io --verify

# Export, transform with pvc-rename-map, apply
crane export --context src --namespace src-ns --export-dir ./export
crane transform --export-dir ./export --transform-dir ./transform \
  --optional-flags '{"pvc-rename-map":"mongodb-data:mongodb-data-new"}'
crane apply --transform-dir ./transform --output-dir ./output

# Verify claimName updated in output
grep 'mongodb-data-new' output/output.yaml

# Apply and scale up
kubectl apply -f output/output.yaml -n src-ns
kubectl scale deployment/mongodb --replicas=1 -n src-ns
```

**Result:** PVC renamed, data transferred, workload references updated via pvc-rename-map. Application healthy after scale-up.

---

### SC-10: SC with enforced capacity limits

**Status:** Skipped

**Commands:**
```bash
# Would require a StorageClass with capacity quota enforcement
crane transfer-pvc \
  --source-context src --destination-context tgt \
  --pvc-name large-pvc:large-pvc-new \
  --dest-storage-class quota-limited-sc \
  --endpoint nginx-ingress --subdomain <node-ip>.nip.io
```

**Result:** Cannot test on minikube — no capacity enforcement available. Needs a real cluster with ResourceQuota or StorageClass capacity limits.

---

### TRN-09: Transform with invalid plugin priority ordering

**Status:** Pass

**Commands:**
```bash
crane export --context src --namespace src-ns --export-dir ./export
crane transform --export-dir ./export --transform-dir ./transform \
  --plugin-priorities "nonexistent-plugin=1"
```

**Result:** No bug found. Crane handled gracefully.

---

### INS-19: Instructions file test case

**Status:** Pass

**Commands:**
```bash
# Create instructions file
cat > instructions.yaml <<EOF
- source-namespace: src-ns
  target-namespace: tgt-ns
  transfer-pvc:
    source-context: src
    destination-context: tgt
    pvc-name: mongodb-data
    endpoint: nginx-ingress
EOF

crane transform --export-dir ./export --transform-dir ./transform \
  --instructions-file instructions.yaml
```

**Result:** No bug found. Instructions file parsed and applied correctly.

---

### PVC-I14: Indirect transfer case

**Status:** Pass

**Commands:**
```bash
crane transfer-pvc \
  --source-context src --destination-context tgt \
  --pvc-name mongodb-data:mongodb-data-new \
  --pvc-namespace src-ns:tgt-ns \
  --cloud-storage "remote:crane-bucket" \
  --rclone-config-file rclone.conf \
  --verify
```

**Result:** No bug found. Indirect transfer via S3/MinIO completed successfully.

---

### PVC-I11: Indirect transfer variant

**Status:** Pass

**Commands:**
```bash
crane transfer-pvc \
  --source-context src --destination-context tgt \
  --pvc-name mongodb-data:mongodb-data-new \
  --pvc-namespace src-ns:tgt-ns \
  --cloud-storage "remote:crane-bucket" \
  --rclone-config-file rclone.conf \
  --encrypt \
  --verify
```

**Result:** No bug found. Encrypted indirect transfer completed successfully.

---

### EXP-08: Export with label selector dependency following

**Status:** Pending — needs team discussion

**Commands:**
```bash
crane export --context src --namespace src-ns \
  --export-dir ./export \
  --label-selector "app=mongodb"
```

**Result:** Design question — should export follow label selectors to discover related resources (Services, ConfigMaps referenced by the selected pods)? Currently exports only resources matching the selector literally. Drafted message for team discussion.

---

### CLI-06: CLI behavior test

**Status:** Pass

**Commands:**
```bash
# Test various CLI flag combinations and error messages
crane transfer-pvc --help
crane transfer-pvc --source-context nonexistent --destination-context tgt \
  --pvc-name test --endpoint route
crane transfer-pvc --source-context src --destination-context tgt \
  --pvc-name test --endpoint nginx-ingress
```

**Result:** No bug found. Error messages were clear and flags validated correctly.

---

### MTA-901: Same-cluster PVC rename + SC conversion (e2e)

**Status:** Automated (PR #875)

**Commands:**
```bash
# Automated via Ginkgo e2e test
ginkgo run -v --focus="\[MTA-901\]" e2e-tests/tests -- \
  --crane-bin=./crane --source-context=src --target-context=tgt
```

**Result:** Full e2e test covering: deploy MongoDB, resolve source SC, create distinct dest SC, scale down, transfer-pvc with rename, export/transform/apply with pvc-rename-map, verify claimName in output, apply to target, validate data, check no leftover resources.

---

## Bug Details

### PVC-D11: AlreadyExists on Destination PVC Skips StorageClass Validation

**Steps to reproduce:**
1. Create a PVC `mydata-new` on StorageClass `sc-old` in the destination namespace
2. Run `crane transfer-pvc --pvc-name mydata:mydata-new --dest-storage-class sc-new`
3. Crane sees AlreadyExists, skips PVC creation, proceeds with transfer
4. Data lands on `sc-old` instead of `sc-new` — no error or warning

**Expected:** Crane should validate that the existing destination PVC matches the requested `--dest-storage-class` and fail/warn if mismatched.

### PVC-D16: rsync Exit 23 Reported as Succeeded

**Steps to reproduce:**
1. Transfer a PVC containing files with mixed ownership (e.g. MongoDB with `.mongodb` owned by root)
2. rsync exits with code 23 (partial transfer — some files not transferred due to permission denied)
3. Crane reports transfer as "succeeded"

**Expected:** Crane should report the transfer with a warning indicating some files could not be transferred.

**Fix applied locally:** Added rsync exit code check in transfer-pvc.go summary defer block:
```go
if rsyncExitCode != nil && *rsyncExitCode != 0 {
    status = "succeeded (with warnings — some files could not be transferred)"
}
```
