# Crane migration guide: application migration from source to target cluster

This guide walks through migrating an application (e.g. `ocp-8pvc-app`, 8 PVCs) from a source OpenShift cluster to a target OpenShift cluster using crane, then validating with the appm CLI. The app name is configurable via `APP_NAME` (default: `ocp-8pvc-app`).

---

## 1. Prerequisites

- **OpenShift CLI:** `oc` and `kubectl` (must be on `PATH`, or set `OC_BIN` / `KUBECTL_BIN`)
- **Crane:** Built by the script from this repo (`go build -o crane .`), or set `CRANE_BIN` to your binary
- **appm CLI:** For validation; set `APPM_VENV_PATH` to the virtualenv directory (default: `$HOME/Documents/git_workdir/oadp-apps-deployer/.my_virtual_env`), then run `source ${APPM_VENV_PATH}/bin/activate && appm validate ...`
- **User-creation script:** Set `IMPROVED_USER_SCRIPT` to the script path (default: `$HOME/Documents/oadp/improved_user.sh`)
- **Cluster access:** kubeadmin credentials for both source and target clusters

All paths and CLIs used by `scripts/migrate-pvcs.sh` can be overridden with environment variables (see script header or table below).

---

## Ways to use this guide

- **Script (recommended):** Run `./scripts/migrate-pvcs.sh` from the repo root. The script performs **steps 2–5** (cluster login and kubeconfig, user/namespace creation, app deploy on source, and all crane migration commands). You only run **validation (step 6)** yourself after the script finishes.
- **Manual:** Sections 2–5 below describe each step if you prefer to run them yourself instead of using the script.

**Important:** Only **user/namespace creation** uses kubeadmin. All other steps (app deploy on source, export, transform, transfer-pvc, apply, oc apply, and validation) run as the **created user** (e.g. `test-user-1`) on both clusters.

---

## 2. Cluster login and kubeconfig setup

*The script does this step automatically. The following is for a fully manual run.*

Log in to both clusters as `kubeadmin` only to create kubeconfig files and (in step 3) to run the user-creation script. After creating the user, log in as that user and use that context for all later steps; the script switches to the created user and never uses kubeadmin again for deploy, export, transfer, or apply.

**Source cluster:**

```bash
oc login https://api.cam-src-70680.qe.devcluster.openshift.com:6443 \
  -u kubeadmin -p <SOURCE_KUBEADMIN_PASSWORD> \
  --insecure-skip-tls-verify
```

**Target cluster:**

```bash
oc login https://api.cam-tgt-70680.qe.devcluster.openshift.com:6443 \
  -u kubeadmin -p <TARGET_KUBEADMIN_PASSWORD> \
  --insecure-skip-tls-verify
```

**Create dedicated kubeconfig files** (so your default `~/.kube/config` is not overwritten):

```bash
# Source
oc login https://api.cam-src-70680.qe.devcluster.openshift.com:6443 \
  -u kubeadmin -p <SOURCE_KUBEADMIN_PASSWORD> \
  --insecure-skip-tls-verify --kubeconfig=/tmp/source-kubeconfig

# Target
oc login https://api.cam-tgt-70680.qe.devcluster.openshift.com:6443 \
  -u kubeadmin -p <TARGET_KUBEADMIN_PASSWORD> \
  --insecure-skip-tls-verify --kubeconfig=/tmp/target-kubeconfig
```

**Merge and create named contexts** for crane `transfer-pvc`:

```bash
KUBECONFIG=/tmp/source-kubeconfig:/tmp/target-kubeconfig kubectl config view --flatten > /tmp/merged-kubeconfig
```

Rename the merged contexts to `source` and `target` (replace the long context names with the ones you see from `kubectl config get-contexts --kubeconfig=/tmp/merged-kubeconfig`):

```bash
kubectl config rename-context "<context-name-containing-cam-src>" source --kubeconfig=/tmp/merged-kubeconfig
kubectl config rename-context "<context-name-containing-cam-tgt>" target --kubeconfig=/tmp/merged-kubeconfig
```

You now have:

- **Source:** `/tmp/source-kubeconfig` (and context `source` in merged)
- **Target:** `/tmp/target-kubeconfig` (and context `target` in merged)
- **Merged:** `/tmp/merged-kubeconfig` with contexts `source` and `target`

---

## 3. User and namespace creation

*The script does this step automatically. The following is for a fully manual run.*

Create the same user and namespace on **both** clusters using the improved_user script. Run it once while logged into the source cluster and once while logged into the target cluster (as kubeadmin).

**Script location:** Set `IMPROVED_USER_SCRIPT` (default: `$HOME/Documents/oadp/improved_user.sh`), or use the path directly when running manually.

**Usage (from the script):**

- `-u` — Username prefix (default: `test-oadp-user`)
- `-n` — Namespace prefix (default: `oadp-test-namespace`)
- `-c` — Number of users to create (default: `1`)
- `-p` — Password for all users (default: `P@ssWord`)

**On the source cluster** (ensure current context is source, e.g. `oc config use-context ...` or use `--kubeconfig=/tmp/source-kubeconfig`):

```bash
# Use IMPROVED_USER_SCRIPT path or default
${IMPROVED_USER_SCRIPT:-$HOME/Documents/oadp/improved_user.sh} -u test-user -n test-ns -c 1 -p 'P@ssWord'
```

This creates user `test-user-1` and namespace `test-ns-1` and grants that user admin in that namespace.

**On the target cluster** (switch to target context or use `--kubeconfig=/tmp/target-kubeconfig`):

```bash
${IMPROVED_USER_SCRIPT:-$HOME/Documents/oadp/improved_user.sh} -u test-user -n test-ns -c 1 -p 'P@ssWord'
```

**Optional:** Log in as `test-user-1` on both clusters and refresh the kubeconfig files so that the merged kubeconfig uses non-admin users for the migration:

```bash
oc login <SOURCE_URL> -u test-user-1 -p 'P@ssWord' --insecure-skip-tls-verify --kubeconfig=/tmp/source-kubeconfig
oc login <TARGET_URL> -u test-user-1 -p 'P@ssWord' --insecure-skip-tls-verify --kubeconfig=/tmp/target-kubeconfig
```

Then recreate the merged kubeconfig and rename contexts to `source` and `target` as in step 2.

---

## 4. Deploy application on source cluster

*The script does this step automatically, as the created user (non-admin). The following is for a fully manual run.*

Set `APP_NAME` (default: `ocp-8pvc-app`) and `APPM_VENV_PATH` if needed. Deploy the app **as the created user** (e.g. `test-user-1`), using the source kubeconfig that has that user as current context, so that crane can export and transfer its resources and PVCs.

**Activate appm and deploy** (set `APPM_VENV_PATH` and `APP_NAME` if different):

```bash
source ${APPM_VENV_PATH:-$HOME/Documents/git_workdir/oadp-apps-deployer/.my_virtual_env}/bin/activate
appm deploy ${APP_NAME:-ocp-8pvc-app} -n test-ns-1
```

This creates the namespace (if needed), PVCs (e.g. `volume1`–`volume8` for `ocp-8pvc-app`), a Deployment that mounts them, and for `ocp-8pvc-app` writes random data plus MD5 checksum files to each volume for later validation.

Ensure the app pod is running and the PVCs are bound before continuing.

---

## 5. Crane migration commands

**If you use the script:** it runs all of the steps in this section (export, transform, transfer-pvc, apply, oc apply). You do not need to do sections 2–4 first; just run the script from the repo root.

**Script (from repo root):**

```bash
# Optional: set APP_NAME, APPM_VENV_PATH, IMPROVED_USER_SCRIPT, SOURCE_CLUSTER_URL, etc.
./scripts/migrate-pvcs.sh
```

The script: (1) builds the crane binary, (2) cluster login and merged kubeconfig (as the created user), (3) user/namespace creation on both clusters, (4) deploys the app on the source via appm (`APP_NAME`), (5) export, (6) transform, (7) transfer-pvc for all PVCs, (8) crane apply, (9) `oc apply` to the target.

**If you are running manually** (after doing steps 2–4 yourself), run the following:

- **Export** (from source cluster; use source kubeconfig):

  ```bash
  ./crane export -e ./export -n test-ns-1 --kubeconfig=/tmp/source-kubeconfig
  ```

- **Transform:**

  ```bash
  ./crane transform -e ./export -t ./transform
  ```

- **Transfer PVCs** (one per volume; use merged kubeconfig so both contexts are available):

  ```bash
  export KUBECONFIG=/tmp/merged-kubeconfig
  for pvc in volume1 volume2 volume3 volume4 volume5 volume6 volume7 volume8; do
    ./crane transfer-pvc \
      --source-context=source --destination-context=target \
      --pvc-name="$pvc" --pvc-namespace=test-ns-1:test-ns-1 \
      --endpoint=route
  done
  ```

- **Apply** (produces manifests under `./output`):

  ```bash
  ./crane apply -e ./export -t ./transform -o ./output
  ```

- **Apply manifests to target cluster:**

  ```bash
  oc apply -f ./output/resources/test-ns-1/ --namespace=test-ns-1 --context=target --kubeconfig=/tmp/merged-kubeconfig
  ```

---

## 6. Validation

After migration, validate the application on the **target** cluster using appm **as the created user (non-admin)**. Use the merged kubeconfig and context `target` (which is the created user on the target cluster).

**Activate appm and validate** (set `APPM_VENV_PATH` if your appm venv is elsewhere):

```bash
source ${APPM_VENV_PATH:-$HOME/Documents/git_workdir/oadp-apps-deployer/.my_virtual_env}/bin/activate
export KUBECONFIG=/tmp/merged-kubeconfig
oc config use-context target
appm validate ${APP_NAME:-ocp-8pvc-app} -n test-ns-1
```

Appm checks that the pod is running and that the MD5 checksums of the data files on each volume match the stored checksums, confirming the PVC data was transferred correctly.

---

## Summary

| Step | Action |
|------|--------|
| 1 | **Prerequisites:** Ensure `oc`, `kubectl`, appm venv, user script, and kubeadmin credentials are available. |
| 2–5 | **Script:** Run `./scripts/migrate-pvcs.sh`. It does: cluster login + kubeconfig merge (2), user/namespace creation with **kubeadmin only here** (3), then **all as created user**: app deploy on source (4), and all crane commands (5). No need to do 2–4 manually first. |
| 6 | **You:** On target **as the created user** (context `target`), run `appm validate ${APP_NAME} -n ${NAMESPACE}` (script prints the exact command with `KUBECONFIG` and context). |

For a fully manual run, do steps 2–4 yourself (use kubeadmin only for user creation; deploy and everything after as the created user), then run the crane commands in section 5 manually.

---

## References

- **User creation script:** `IMPROVED_USER_SCRIPT` (default: `$HOME/Documents/oadp/improved_user.sh`)
- **appm CLI:** `APPM_VENV_PATH` (default: `$HOME/Documents/git_workdir/oadp-apps-deployer/.my_virtual_env`) — activate then `appm deploy` / `appm validate`
- **Crane script:** [scripts/migrate-pvcs.sh](../scripts/migrate-pvcs.sh)
- **Example run (cam-src/tgt-70930, ocp-8pvc-app in oadp-apps-deployer):** [scripts/run-migration-70930-ocp-8pvc.sh](../scripts/run-migration-70930-ocp-8pvc.sh) — sets cluster URLs, passwords, namespace, and uses dest storage class from target

### Script configuration (environment variables)

| Variable | Default | Description |
|----------|---------|-------------|
| `APP_NAME` | `ocp-8pvc-app` | Application to deploy on source (appm) and validate on target |
| `DEST_STORAGE_CLASS` | _(empty)_ | Destination storage class; if unset, script auto-detects from target (default SC or first available) |
| `IMPROVED_USER_SCRIPT` | `$HOME/Documents/oadp/improved_user.sh` | Path to user/namespace creation script |
| `OC_BIN` | `oc` | Path to OpenShift CLI |
| `KUBECTL_BIN` | `kubectl` | Path to kubectl |
| `GO_BIN` | `go` | Path to Go compiler (for building crane) |
| `CRANE_BIN` | `./crane` | Path to crane binary (relative to repo root when script runs) |
| `APPM_VENV_PATH` | `$HOME/Documents/git_workdir/oadp-apps-deployer/.my_virtual_env` | Path to appm virtualenv (deploy on source + validation hint) |
| `SOURCE_KUBECONFIG` | `/tmp/source-kubeconfig` | Path to source-cluster kubeconfig |
| `TARGET_KUBECONFIG` | `/tmp/target-kubeconfig` | Path to target-cluster kubeconfig |
| `MERGED_KUBECONFIG` | `/tmp/merged-kubeconfig` | Path to merged kubeconfig with `source` / `target` contexts |

Other variables (cluster URLs, passwords, `NAMESPACE`, `USER_NAME`, `USER_PASS`, `PVC_LIST`, `ENDPOINT_TYPE`, `EXPORT_DIR`, `TRANSFORM_DIR`, `OUTPUT_DIR`) are also overridable; see the script header.