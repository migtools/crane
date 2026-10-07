# export: CRD write fails when _cluster directory is not created

> **GitHub issue title (use in title field):** `export: CRD write fails when _cluster directory is not created`

## Summary

- **Problem:** `crane export` can fail with **`no such file or directory`** when writing a **`CustomResourceDefinition`** under `resources/<namespace>/_cluster/`.
- **Cause:** `_cluster` is only created when an earlier step decides cluster-scoped manifests exist. **CRDs are added later** in the same run but still write to `_cluster`, so the directory may **never be created**.
- **Impact:** Valid namespaces that only trigger CRD export (no other `_cluster` content) can **fail the whole export**.

## Steps to reproduce

1. Create namespace, CRD, and CR:

```bash
export CTX=<your-context>
export NS=crane-exp-crd-repro

kubectl create namespace "$NS" --context "$CTX" --dry-run=client -o yaml | kubectl apply --context "$CTX" -f -

kubectl apply --context "$CTX" -f - <<'EOF'
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.craneexplore.test
spec:
  group: craneexplore.test
  scope: Namespaced
  names:
    plural: widgets
    singular: widget
    kind: Widget
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                note:
                  type: string
EOF

kubectl apply --context "$CTX" -n "$NS" -f - <<'EOF'
apiVersion: craneexplore.test/v1
kind: Widget
metadata:
  name: widget-one
spec:
  note: "repro"
EOF
```

2. Export to a clean directory:

```bash
rm -rf ./exp-repro && mkdir ./exp-repro
crane export -e ./exp-repro --namespace "$NS" --context "$CTX"
```

## Actual behavior

| Item | Detail |
|------|--------|
| **Logs** | Indicate `_cluster` is empty / not needed for the initial cluster-scoped pass |
| **Failure** | Open `.../resources/<namespace>/_cluster/CustomResourceDefinition_*.yaml` fails (**parent dir missing**) |
| **Exit** | Non-zero |

## Expected behavior

- **`_cluster`** exists **before** any write to `.../_cluster/*.yaml`, **or**
- CRD output uses a path that is **always** created,

so this scenario completes **without** a missing-directory error.

## Environment

- **Crane:** `main` / version: _(fill in)_
- **Cluster:** e.g. minikube (not cloud-specific)
