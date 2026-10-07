# Crane migration: read-only user scenario

This guide describes a **demonstration scenario** that gives a user **read-only (view) permissions** on both source and target clusters, then runs the crane migration pipeline. The goal is to show that migration **requires write permissions**: `crane export` and `crane transform` succeed, but **`crane transfer-pvc` fails** because the view role cannot create PVCs, Transfer objects, pods, services, or routes.

This scenario does **not** use the main [crane-migration-guide](crane-migration-guide.md) user-creation script (`improved_user.sh`). The read-only script creates the user and namespace **inline** and assigns the **view** ClusterRole instead of **admin**.

---

## 1. Prerequisites

- **OpenShift CLI:** `oc` and `kubectl` (or set `OC_BIN` / `KUBECTL_BIN`)
- **Crane:** Built by the script from this repo, or set `CRANE_BIN`
- **htpasswd:** On `PATH` (used to add the read-only user to the cluster’s htpasswd secret)
- **appm CLI:** For deploying the app on source as kubeadmin; set `APPM_VENV_PATH` (default: `$HOME/Documents/git_workdir/oadp-apps-deployer/.my_virtual_env`)
- **Cluster access:** kubeadmin credentials for both source and target clusters
- **Htpasswd OAuth:** Both clusters must have `htpass-secret` in the `openshift-config` namespace (standard for htpasswd-based identity provider)

All paths and credentials can be overridden with environment variables (see script header or table at the end).

---

## 2. What the script does

The script **`scripts/migrate-pvcs-readonly.sh`** runs the following steps and **stops at the first failure** (expected at `crane transfer-pvc`):

| Step | Action | Expected result |
|------|--------|-----------------|
| 1 | Build crane binary | Success |
| 2 | Log in as kubeadmin on both clusters | Success |
| 2 | Create namespace and **view-role** user on both clusters (inline; no `improved_user.sh`) | Success |
| 2 | Deploy app on source **as kubeadmin** (read-only user cannot create resources) | Success |
| 2 | Wait for OAuth, then switch kubeconfigs to the read-only user and merge (contexts `source` / `target`) | Success |
| 4 | **crane export** (read-only user can list/get namespace resources) | Success |
| 5 | **crane transform** (local files) | Success |
| 6 | **crane transfer-pvc** (first PVC only) | **Failure** — user cannot create PVCs or other resources |

The script then prints a clear “Migration step failed (expected for read-only user)” message and exits. It does **not** run `crane apply` or `oc apply`.

---

## 3. How to run the scenario

From the **repo root**:

```bash
# Default user/namespace (test-username, test-namespace)
./scripts/migrate-pvcs-readonly.sh
```

To use a **dedicated user and namespace** (recommended so you don’t clash with existing test users):

```bash
USER_NAME=readonly-user NAMESPACE=readonly-ns ./scripts/migrate-pvcs-readonly.sh
```

Override cluster URLs and kubeadmin passwords if your clusters differ from the script defaults:

```bash
SOURCE_CLUSTER_URL=https://api.cam-src-70680.qe.devcluster.openshift.com:6443 \
TARGET_CLUSTER_URL=https://api.cam-tgt-70680.qe.devcluster.openshift.com:6443 \
SOURCE_KUBEADMIN_PASS='<source-kubeadmin-password>' \
TARGET_KUBEADMIN_PASS='<target-kubeadmin-password>' \
USER_NAME=readonly-user \
NAMESPACE=readonly-ns \
./scripts/migrate-pvcs-readonly.sh
```

---

## 4. Expected output

- **Export:** May log “cannot list” for some cluster-scoped resources the view user cannot see; namespace-scoped resources (PVCs, deployments, pods, etc.) are still exported.
- **Transform:** Succeeds (local file operations).
- **Transfer-pvc:** Fails with a permission error, for example:

  ```
  persistentvolumeclaims is forbidden: User "readonly-user" cannot create resource "persistentvolumeclaims" in API group "" in the namespace "readonly-ns"
  unable to create destination PVC
  ```

The script then prints:

```
==============================================
 Migration step failed (expected for read-only user)
 Step: crane transfer-pvc
 Reason: read-only user cannot create Transfer objects, pods, services, or routes on clusters (view role has no create/update/delete)
==============================================
```

---

## 5. Manual equivalent (optional)

If you prefer to run the scenario manually instead of using the script:

1. Log in as kubeadmin to both clusters and create separate kubeconfig files.
2. On each cluster:
   - Create the namespace: `oc create ns <NAMESPACE>`
   - Add the user to htpasswd: decode `openshift-config/htpass-secret`, run `htpasswd -bB`, replace the secret
   - Create a **view** rolebinding: `oc create rolebinding <USER>-namespace-view --clusterrole=view --user=<USER> --namespace=<NAMESPACE>`
3. Deploy the app on the **source** cluster as kubeadmin (e.g. `appm deploy ocp-8pvc-app -n <NAMESPACE>` with `KUBECONFIG` set to the source kubeconfig where you are kubeadmin).
4. Wait for OAuth to pick up the new user, then log in as that user on both clusters and build a merged kubeconfig with contexts `source` and `target`.
5. As the read-only user, run:
   - `crane export -e ./export -n <NAMESPACE> --kubeconfig=<source-kubeconfig>` — should succeed
   - `crane transform -e ./export -t ./transform` — should succeed
   - `crane transfer-pvc ...` — will fail with a “cannot create” permission error.

---

## 6. Summary

| Item | Description |
|------|-------------|
| **Purpose** | Show that crane migration requires write permissions; a view-only user fails at transfer-pvc. |
| **Script** | [scripts/migrate-pvcs-readonly.sh](../scripts/migrate-pvcs-readonly.sh) |
| **User creation** | Inline in the script (view role only); does **not** use `improved_user.sh`. |
| **App deploy** | Done as **kubeadmin** on source before switching to the read-only user. |
| **Expected failure** | `crane transfer-pvc` when run as the read-only user. |

---

## 7. Script configuration (environment variables)

| Variable | Default | Description |
|----------|---------|-------------|
| `USER_NAME` | `test-username` | Read-only user to create on both clusters |
| `NAMESPACE` | `test-namespace` | Namespace to create on both clusters |
| `USER_PASS` | `P@ssWord` | Password for the read-only user |
| `SOURCE_CLUSTER_URL` | (script default) | Source cluster API URL |
| `TARGET_CLUSTER_URL` | (script default) | Target cluster API URL |
| `SOURCE_KUBEADMIN_PASS` | (script default) | Source kubeadmin password |
| `TARGET_KUBEADMIN_PASS` | (script default) | Target kubeadmin password |
| `APP_NAME` | `ocp-8pvc-app` | App to deploy on source (as kubeadmin) |
| `APPM_VENV_PATH` | `$HOME/Documents/git_workdir/oadp-apps-deployer/.my_virtual_env` | appm virtualenv for deploy |
| `OC_BIN` | `oc` | OpenShift CLI |
| `KUBECTL_BIN` | `kubectl` | kubectl |
| `CRANE_BIN` | `./crane` | Crane binary (relative to repo root) |
| `MERGED_KUBECONFIG` | `/tmp/merged-kubeconfig-readonly` | Merged kubeconfig (separate from main migration script) |

Other variables (`PVC_LIST`, `ENDPOINT_TYPE`, `EXPORT_DIR`, `TRANSFORM_DIR`, `OUTPUT_DIR`, `WAIT_USER_TIMEOUT`, `WAIT_USER_INTERVAL`, and kubeconfig paths) are also overridable; see the script header.

---

## 8. References

- **Main migration guide (admin user):** [crane-migration-guide.md](crane-migration-guide.md)
- **Read-only scenario script:** [scripts/migrate-pvcs-readonly.sh](../scripts/migrate-pvcs-readonly.sh)
