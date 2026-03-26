.PHONY: setup-minikube-same-network delete-minikube-same-network

setup-minikube-same-network:
	bash ./hack/setup-minikube-same-network.sh

delete-minikube-same-network:
	minikube delete -p src || true
	minikube delete -p tgt || true
