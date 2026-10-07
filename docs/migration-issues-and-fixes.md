# Issues found and fixes during crane migration

Issues encountered while running `crane transfer-pvc` (and the broader migration workflow) and how they were addressed.

---

## 1. Rsync authentication failure (fixed in crane)

**Symptom:** When running `crane transfer-pvc` to copy PVC data from source to target cluster, the rsync client pod on the source cluster failed with:

```
@ERROR: auth failed on module <module-id>
rsync error: error starting client-server protocol (code 5)
```

The client pod never became ready because the rsync daemon on the target rejected the client: the client had no `RSYNC_PASSWORD` (no value, no `SecretKeyRef`), while the server expected password-based auth. The library was supposed to create a secret on the source (`backube-rsync-password-*`) and wire the client pod to it via `reconcilePassword`, but in cross-cluster scenarios that secret was not created (or not used), so auth always failed.

**Root cause:** Crane used an old version of `github.com/backube/pvc-transfer` (Jul 18, 2022) that relied on rsync password authentication. That path was buggy in cross-cluster use.

**Fix (upstream):** Upstream removed rsync-level password auth entirely in commit [75b9e4c](https://github.com/backube/pvc-transfer/commit/75b9e4cf10079c25e36aa642b82bb80d4442a532) — **"remove rsync password"** (Aug 2, 2022, author: pranavgaikwad). The stunnel TLS tunnel between source and target already provides authentication and encryption, so the rsync password was redundant and was removed on both client and server.

**Fix (crane):** Bump `github.com/backube/pvc-transfer` to a version that includes that commit (e.g. `v0.0.0-20220810121213-5f9e29a1f6e5`) and remove all rsync password handling from `cmd/transfer-pvc/transfer-pvc.go` (no `getRsyncPassword()`, no password args to `NewServer`/`NewClient`). See the PR or commits that introduced the dependency bump and the corresponding code changes.

**Reference:**  

- Upstream commit: [https://github.com/backube/pvc-transfer/commit/75b9e4cf10079c25e36aa642b82bb80d4442a532](https://github.com/backube/pvc-transfer/commit/75b9e4cf10079c25e36aa642b82bb80d4442a532)  
- Repo: [https://github.com/backube/pvc-transfer](https://github.com/backube/pvc-transfer)

---

## 2. crane transfer-pvc and kubeconfig

**Issue:** The `transfer-pvc` subcommand does not expose a `--kubeconfig` flag (it uses `genericclioptions.ConfigFlags` but does not add them to the cobra command). With a default or wrong kubeconfig, the command can fail or talk to the wrong cluster.

**Workaround:** Set the `KUBECONFIG` environment variable when running the command, e.g. `KUBECONFIG=/tmp/merged-kubeconfig ./crane transfer-pvc ...`.

---

## 3. Merged kubeconfig context names

**Issue:** After merging source and target kubeconfigs, context names are long and cluster-specific. For `transfer-pvc` you need contexts named `source` and `target`. If the merged file contains both kubeadmin and the created user (e.g. `test-user-1`) for each cluster, renaming *every* context that contains `cam-src` to `source` (and similarly for target) fails when the second such context is renamed, because `source` already exists.

**Workaround:** Rename only the contexts that correspond to the created user (e.g. contexts whose name contains both the cluster hint and the username), so that exactly one context becomes `source` and one becomes `target`. The migration script does this.

---

## 4. Cluster name in kubeconfig

**Issue:** The `context.cluster` field in the kubeconfig must exactly match the `name` of a cluster in the `clusters` section (e.g. `api-cam-src-70680-qe-devcluster-openshift-com:6443` with a hyphen, not `openshift.com`). If they differ, `kubectl`/`oc` can fail with "cannot locate cluster" or try to use `localhost:8080`.

**Workaround:** Ensure merged or hand-edited kubeconfigs keep cluster names consistent (e.g. use the same hyphenated form as in the cluster `name`).

---

## Summary


| Issue                           | Type        | Resolution                                        |
| ------------------------------- | ----------- | ------------------------------------------------- |
| Rsync auth failure              | Code fix    | Bump `backube/pvc-transfer`; remove password code |
| transfer-pvc kubeconfig         | Workaround  | Use `KUBECONFIG` env when calling transfer-pvc    |
| Merged context rename clash     | Script      | Rename only user contexts to `source`/`target`    |
| Cluster name mismatch in config | Operational | Keep context cluster name = cluster name          |


