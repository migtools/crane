# Validating manifests before applying to your cluster

## Why validate before applying?

Applying unvalidated manifests to a cluster can lead to partial deployments, broken workloads, or security misconfigurations that are difficult to roll back. A failed `kubectl apply` halfway through a large migration may leave the cluster in an inconsistent state — some resources created, others rejected — requiring manual cleanup. By validating before you apply, you catch API incompatibilities, missing dependencies, and permission gaps up front, saving significant debugging time and reducing the risk of downtime on your target cluster.

Use this checklist when you are about to apply Kubernetes manifests to a **target** cluster.   
  
For example, output from the crane pipeline (`export` → `transform` → `apply` phases produce YAML under a directory such as `./output/resources/`). The goal is to catch YAML issues, API mismatches, RBAC gaps, and missing dependencies **before** a real `kubectl apply` or `oc apply`.

**What you need:** `kubectl` or `oc` configured against the target cluster, and a path to your manifest directory (below we use `./output/resources/`; change it to match yours).

---

## 1. Client dry-run (syntax and local schema)

Runs entirely on your machine: parses YAML and checks against the schema shipped with your client.

```bash
kubectl apply --dry-run=client -f ./output/resources/ --recursive
```

Fix any parse errors, unknown fields, or obvious type problems before continuing.

**Limits:** Your client may not fully validate arbitrary CRDs. You may also see `no matches for kind … in version …` here if the API version or CRD is not known to your client—treat that like an API/compatibility issue and fix manifests or install CRDs on the target as needed.

---

## 2. Server dry-run (target API and admission)

Sends manifests to the **target** API server without persisting them. This validates real API versions, CRDs, quotas, admission policies, and conflicts with existing objects.

```bash
kubectl apply --dry-run=server -f ./output/resources/ --recursive
```

**Reading common errors**


| If you see…                          | It usually means…                                                          |
| ------------------------------------ | -------------------------------------------------------------------------- |
| `no matches for kind … in version …` | Wrong or removed API version, or CRD not installed on the target           |
| `admission webhook … denied`         | Policy/controller blocked the request (e.g. security policy)               |
| `exceeded quota`                     | Quota would be exceeded in that namespace                                  |
| `field is immutable`                 | Object already exists and that field cannot be changed this way            |
| `namespaces "…" not found`           | Namespace does not exist yet; create it or apply Namespace manifests first |


**Tip:** If you apply a **Namespace** and namespaced resources in the **same** server dry-run, the namespace may not appear “ready” for sibling objects in that single pass. Creating namespaces first (or a second dry-run/apply pass) often clears that.

---

## 3. Permissions (`kubectl auth can-i`)

Confirm the identity you use for migration can **create** (and if needed **patch** / **update**) each resource type you are about to apply. That avoids half-applied runs blocked by RBAC.

`kubectl auth can-i` uses the **plural** resource name (e.g. `deployments`, not `Deployment`). On the target cluster:

```bash
kubectl api-resources
# OpenShift: oc api-resources
```

Find the **NAME** (plural) and **KIND** for each type in your YAML. For custom resources, use the plural from the CRD.

**Pattern**

- Namespaced object: `kubectl auth can-i create <plural> -n <namespace>`
- Cluster-scoped object: `kubectl auth can-i create <plural>` (no `-n`)

You want `yes` for each check that matches your apply plan. To test another user or service account (if your kube user may impersonate):

```bash
kubectl auth can-i create deployments -n my-namespace --as=system:serviceaccount:my-namespace:my-sa
```

---

## 4. References between resources

Dry-run does **not** prove that ConfigMaps, Secrets, ServiceAccounts, Roles, Services, or StorageClasses **exist** when workloads need them. After dry-runs pass, spot-check critical references with `kubectl get`, for example:

```bash
kubectl get configmap <name> -n <namespace>
kubectl get secret <name> -n <namespace>
kubectl get serviceaccount <name> -n <namespace>
kubectl get storageclass <name>
```

If everything was exported together, references are usually satisfied; gaps often come from excluded resources or cluster-only dependencies.

---

## 5. Namespaces

List namespaces mentioned in your tree, then ensure they exist (or apply Namespace manifests first):

```bash
grep -rh "namespace:" ./output/resources/ | sort -u
```

Create missing ones as appropriate for your process, or apply namespace YAML before the rest.

---

## 6. Apply order (when one shot fails)

`kubectl apply -R` does not guarantee dependency order. In practice, problems show up when:

- Namespaced resources are applied before the **Namespace** exists
- **Custom resources** are applied before their **CRD** is established

**Practical mitigations**

- Apply **Namespaces** (and **CRDs**, waiting until established if needed, e.g. `kubectl wait --for=condition=established crd/<name> --timeout=…`) before dependents.
- Or run apply **twice**: the second pass picks up objects that failed the first time because a dependency was missing.
- Prefer splitting large flat trees into ordered batches (infra → RBAC → config → workloads) when you hit ordering errors.

---

## Quick validation script

Save the script below as `validate-manifests.sh` and run it with the path to your manifest directory:

```bash
./validate-manifests.sh ./output/resources
```

**`validate-manifests.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

OUTPUT_DIR="${1:?Usage: $0 <manifest-directory>}"

if [ ! -d "$OUTPUT_DIR" ]; then
  echo "ERROR: Directory '$OUTPUT_DIR' does not exist." >&2
  exit 1
fi

echo "=== 1. Client dry-run (syntax and local schema) ==="
if kubectl apply --dry-run=client -f "$OUTPUT_DIR" --recursive; then
  echo "PASS: Client dry-run succeeded."
else
  echo "FAIL: Fix the errors above before continuing." >&2
  exit 1
fi

echo ""
echo "=== 2. Server dry-run (target API and admission) ==="
if kubectl apply --dry-run=server -f "$OUTPUT_DIR" --recursive; then
  echo "PASS: Server dry-run succeeded."
else
  echo "FAIL: Fix the errors above before continuing." >&2
  exit 1
fi

echo ""
echo "=== 3. Permissions check ==="
RESOURCE_TYPES=$(kubectl api-resources --no-headers -o name 2>/dev/null | sort -u)
NAMESPACES=$(grep -rh 'namespace:' "$OUTPUT_DIR" 2>/dev/null | awk '{print $2}' | sort -u)
FAILED=0

for ns in $NAMESPACES; do
  for rt in $RESOURCE_TYPES; do
    if grep -rql "kind:.*$(echo "$rt" | sed 's/\..*//')" "$OUTPUT_DIR" 2>/dev/null; then
      if ! kubectl auth can-i create "$rt" -n "$ns" --quiet 2>/dev/null; then
        echo "NO: cannot create $rt in namespace $ns"
        FAILED=1
      fi
    fi
  done
done

if [ "$FAILED" -eq 0 ]; then
  echo "PASS: Permissions look good for detected resource types."
else
  echo "WARN: Some permission checks failed. Review the output above."
fi

echo ""
echo "Validation complete. If all steps passed, proceed with apply."
echo "For large migrations, also review sections 4-6 of the validation guide."
```

When steps 1 and 2 are clean and 3 shows no failures, proceed with apply — still using sections 4–6 if your migration is large or has CRDs, RBAC, or many namespaces.

---

## What validation cannot prove

- **Runtime behavior** (probes, init containers, external services) only shows up after pods run.
- **Dry-run** does not run controllers or schedules workloads.
- **Cross-object references** need the manual checks in section 4.
- **Ordering** may require multiple apply passes or ordered directories even when dry-runs pass.

