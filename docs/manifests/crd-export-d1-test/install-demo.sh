#!/usr/bin/env bash
# Install the mixed app for D1 cluster testing (reuses crane-complex-demo manifests).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPLEX_DEMO="${SCRIPT_DIR}/../crane-complex-demo"

kubectl apply -f "${SCRIPT_DIR}/00-namespace.yaml"
kubectl apply -f "${COMPLEX_DEMO}/00-crd-widget.yaml"

if kubectl api-resources --api-group=security.openshift.io 2>/dev/null | grep -q securitycontextconstraints; then
  echo "Applying OpenShift SCC manifest..."
  kubectl apply -f "${COMPLEX_DEMO}/01-scc.yaml"
else
  echo "Skipping 01-scc.yaml (security.openshift.io not available — plain Kubernetes)."
fi

kubectl apply -f "${COMPLEX_DEMO}/02-namespace-app.yaml"
kubectl apply -f "${SCRIPT_DIR}/03-secret-dummy.yaml"

echo "Waiting for Deployment..."
kubectl wait --for=condition=Available "deployment/complex-demo-app" -n crane-complex-demo --timeout=120s

echo "Done. Namespace: crane-complex-demo | CRD name: widgets.stable.example.com | GVR: stable.example.com/v1 widgets"
