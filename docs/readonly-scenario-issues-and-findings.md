# Issues and findings: read-only user migration scenario

Notes from running the [read-only user migration scenario](crane-migration-readonly-scenario.md) (`scripts/migrate-pvcs-readonly.sh`). Two real issues in this scenario:

---

## 1. transfer-pvc fails with view role (expected)

**Observation:** When the migration is run as a user with only the **view** ClusterRole (read-only), `crane transfer-pvc` fails with a permission error such as:

```
persistentvolumeclaims is forbidden: User "readonly-user" cannot create resource "persistentvolumeclaims" in API group "" in the namespace "readonly-ns"
unable to create destination PVC
```

**Cause:** The view role grants only `get`, `list`, and `watch`. Transfer-pvc must **create** resources on both clusters (destination PVCs, Transfer CRs, rsync pods, services, routes). A view-only user cannot create any of these.

**Resolution:** This is expected. Use a user with write access in the namespace (e.g. **admin** role via `improved_user.sh`) for real migrations. The read-only scenario is for demonstration only.

---

## 2. Application deployment must be done as kubeadmin

**Observation:** A read-only (view) user cannot create Deployments, PVCs, Pods, or other resources in the namespace. So the application (e.g. `ocp-8pvc-app`) cannot be deployed by that user.

**Cause:** Deploying the app requires create/write permissions. The view role has no create, update, or delete rights.

**Resolution:** In this scenario, the app must be deployed on the source cluster **as kubeadmin** (or another user with write access) before switching to the read-only user for the migration steps. The script does this: it deploys the app as kubeadmin, then switches to the read-only user for export, transform, and transfer-pvc (where transfer-pvc then fails as in issue 1).

---

## Summary

| Issue | Resolution |
|-------|------------|
| transfer-pvc fails with view role | Expected; use admin/write role for real migrations |
| App deployment requires kubeadmin | Deploy app as kubeadmin before using read-only user for migration steps |
