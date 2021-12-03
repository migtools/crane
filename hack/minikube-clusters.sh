export KUBECONFIG=${PWD}/state-transfer.kubeconfig

SRC_CLUSTER_NAME=source-cluster
DEST_CLUSTER_NAME=destination-cluster

minikube start -p ${SRC_CLUSTER_NAME}
minikube start -p ${DEST_CLUSTER_NAME}

SOURCE_IP=$(minikube ip -p ${SRC_CLUSTER_NAME})
DEST_IP=$(minikube ip -p ${DEST_CLUSTER_NAME})
SOURCE_IP_RANGE=$(echo $SOURCE_IP | cut -d"." -f1-3).0/24 # subnet 11.1.1.0/24
DEST_IP_RANGE=$(echo $DEST_IP | cut -d"." -f1-3).0/24 # subnet 11.1.1.0/24

sudo iptables -I FORWARD 2 -p all -s $SOURCE_IP_RANGE -d $DEST_IP_RANGE -j ACCEPT
sudo iptables -I FORWARD 3 -p all -s $DEST_IP_RANGE -d $SOURCE_IP_RANGE -j ACCEPT

minikube ssh -p ${SRC_CLUSTER_NAME} sudo ip r add $DEST_IP_RANGE via $(echo $SOURCE_IP | cut -d"." -f1-3).1
minikube ssh -p ${DEST_CLUSTER_NAME} sudo ip r add $SOURCE_IP_RANGE via $(echo $DEST_IP | cut -d"." -f1-3).1

minikube ssh -p ${SRC_CLUSTER_NAME} "ping -c 4 ${DEST_IP}"
if [ "$?" != 0 ];
then
  echo "unable to set up networking"
  exit 1
fi

echo "network setup successful, configuring nginx ingress on destionation cluster"
minikube addons -p ${DEST_CLUSTER_NAME} enable ingress

minikube update-context -p ${SRC_CLUSTER_NAME}

# this hack does not work if the script is run twice
COREFILE=$(kubectl get cm  -n kube-system coredns  -ojson | jq '.data.Corefile')
COREFILE=$(echo $COREFILE | sed s/'fallthrough\\n }\\n/& file \/etc\/coredns\/crane.db crane.dev\\n/')
kubectl get cm -n kube-system coredns -ojson | jq ".data.Corefile = ${COREFILE}" | kubectl replace -f -

kubectl patch cm -n kube-system coredns --type='json' -p='[{"op": "replace", "path": "/data/crane.db", "value": "; crane.dev test file\ncrane.dev.              IN      SOA     a.crane.dev. b.crane.dev. 2 604800 86400 2419200 604800\ncrane.dev.              IN      NS      a.crane.dev.\ncrane.dev.              IN      NS      b.crane.dev.\na.crane.dev.            IN      A       127.0.0.1\nb.crane.dev.            IN      A       127.0.0.1\n\n*.crane.dev.            IN      A       DEST_IP\n"}]'
kubectl get cm -n kube-system coredns -oyaml | sed "s/DEST_IP/${DEST_IP}/" | kubectl replace -f -

kubectl patch deploy -n kube-system coredns --type='json' -p='[{"op": "add", "path": "/spec/template/spec/volumes/0/configMap/items/1", "value": {"key": "crane.db", "path": "crane.db"}}]'

kubectl patch deploy --context=destination-cluster -n ingress-nginx ingress-nginx-controller --type='json' -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/12", "value": "--enable-ssl-passthrough"}]'

# force a rollout
kubectl delete rs -n ingress-nginx --context=destination-cluster  -l app.kubernetes.io/component=controller,app.kubernetes.io/instance=ingress-nginx