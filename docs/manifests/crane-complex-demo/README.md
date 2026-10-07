# crane-complex-demo manifests

For a scripted **D1 CRD export** test (install + `crane export` checks), see [`../crd-export-d1-test/README.md`](../crd-export-d1-test/README.md).

Apply **as cluster-admin** in order:

1. `00-crd-widget.yaml` — `CustomResourceDefinition` (cluster).
2. `oc new-project crane-complex-demo` (or create namespace).
3. `01-scc.yaml` — `SecurityContextConstraints` referencing `system:serviceaccount:crane-complex-demo:app-sa`.
4. `02-namespace-app.yaml` — ServiceAccount, ConfigMap, Widget CR, ClusterRole, ClusterRoleBinding, Deployment, Service.

```bash
oc apply -f docs/manifests/crane-complex-demo/00-crd-widget.yaml
oc new-project crane-complex-demo   # or: oc create namespace crane-complex-demo
oc apply -f docs/manifests/crane-complex-demo/01-scc.yaml
oc apply -f docs/manifests/crane-complex-demo/02-namespace-app.yaml
oc wait --for=condition=Available deployment/complex-demo-app -n crane-complex-demo --timeout=120s
```

Cleanup (cluster-admin): delete Widget CR and namespace first, then SCC, then CRD.
