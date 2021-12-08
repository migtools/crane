#!/usr/bin/env bash
set +x

SRC_CLUSTER_NAME=src
DEST_CLUSTER_NAME=dest

SOURCE_IP=$(minikube ip -p ${SRC_CLUSTER_NAME})
DEST_IP=$(minikube ip -p ${DEST_CLUSTER_NAME})
SOURCE_IP_RANGE="${SOURCE_IP%.*}.0/24"
DEST_IP_RANGE="${DEST_IP%.*}.0/24"

sudo iptables -D FORWARD -p all -s $SOURCE_IP_RANGE -d $DEST_IP_RANGE -j ACCEPT
sudo iptables -D FORWARD -p all -s $DEST_IP_RANGE -d $SOURCE_IP_RANGE -j ACCEPT

minikube delete -p ${SRC_CLUSTER_NAME}
minikube delete -p ${DEST_CLUSTER_NAME}

