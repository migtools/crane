#!/bin/bash

set -e

echo "=== 1. Cleaning up previous migration environments ==="
kubectl delete ns tdinavet-migration --context src --ignore-not-found=true
kubectl delete ns tdinavet-migration --context tgt --ignore-not-found=true
echo "Waiting for cleanup to finalize..."
sleep 5

echo "=== 2. Preparing Target Cluster (tgt) Ingress ==="
minikube addons enable ingress -p tgt

echo "Waiting for Ingress Controller deployment to be available..."
kubectl wait -n ingress-nginx --for=condition=available deployment/ingress-nginx-controller --timeout=300s --context=tgt

echo "Patching Ingress with SSL Passthrough and hostPort 443..."
kubectl patch deployment ingress-nginx-controller -n ingress-nginx --context=tgt --type='json' -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--enable-ssl-passthrough"}]'
kubectl patch deployment ingress-nginx-controller -n ingress-nginx --context=tgt --type='json' -p='[{"op": "add", "path": "/spec/template/spec/containers/0/ports/1/hostPort", "value": 443}]'

echo "Waiting for Ingress Rollout to finish..."
kubectl rollout status deployment/ingress-nginx-controller -n ingress-nginx --context=tgt
sleep 3

echo "=== 3. Testing connectivity between clusters ==="
kubectl run curl-verify --rm -it --restart=Never --image=curlimages/curl --context src -- curl -k -s -o /dev/null -w "Target Cluster HTTP Status Response: %{http_code}\n" --connect-timeout 10 https://172.18.0.3
sleep 2

echo "=== 4. Creating Namespaces and Source PVC ==="
kubectl create ns tdinavet-migration --context src
kubectl create ns tdinavet-migration --context tgt

kubectl apply --context src -f - <<EOF
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: data-pvc
  namespace: tdinavet-migration
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 1Gi
EOF

echo "Waiting for PVC to be provisioned..."
sleep 5

echo "=== 5. Executing PVC Migration via Crane ==="
# הרצה חוסמת - הסקריפט ייעצר כאן ולא ימשיך עד ש-Crane לא יסיים לחלוטין!
./crane-migrator transfer-pvc \
  --source-context=src \
  --destination-context=tgt \
  --pvc-name=data-pvc \
  --pvc-namespace=tdinavet-migration:tdinavet-migration \
  --endpoint=nginx-ingress \
  --ingress-class=nginx \
  --subdomain=data-pvc.tdinavet-migration.172.18.0.3.nip.io

echo ""
echo "=== MIGRATION PROCESS COMPLETE ==="
