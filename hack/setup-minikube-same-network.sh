#!/usr/bin/env bash
set -euo pipefail
set -x

# Setup from scratch for same-docker-network Minikube clusters:
# 1) create docker network
# 2) inspect subnet
# 3) start src on that network, read src IP
# 4) re-start src with --static-ip=<src-ip>
# 5) start tgt with --static-ip=<src-ip last-octet + 1>
# 6) setup ingress on tgt (ssl passthrough + hostPort 443)
#
# Config:
#   NETWORK_NAME       default: minikube-mc
#   SRC_PROFILE        default: src
#   TGT_PROFILE        default: tgt
#   MINIKUBE_DRIVER    default: docker
#   MINIKUBE_CPUS      optional (example: 2)
#   MINIKUBE_MEMORY    optional in MB (example: 4096)
#   SRC_K8S_VERSION    optional
#   TGT_K8S_VERSION    optional
#   INGRESS_WAIT       default: 300s
#   RESET_PROFILES     default: true
#   RECREATE_NETWORK   default: true

NETWORK_NAME="${NETWORK_NAME:-minikube-mc}"
SRC_PROFILE="${SRC_PROFILE:-src}"
TGT_PROFILE="${TGT_PROFILE:-tgt}"
MINIKUBE_DRIVER="${MINIKUBE_DRIVER:-docker}"
MINIKUBE_CPUS="${MINIKUBE_CPUS:-}"
MINIKUBE_MEMORY="${MINIKUBE_MEMORY:-}"
SRC_K8S_VERSION="${SRC_K8S_VERSION:-}"
TGT_K8S_VERSION="${TGT_K8S_VERSION:-}"
INGRESS_WAIT="${INGRESS_WAIT:-300s}"
RESET_PROFILES="${RESET_PROFILES:-true}"
RECREATE_NETWORK="${RECREATE_NETWORK:-true}"
set -x

log() {
  printf '[setup-minikube] %s\n' "$*"
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf 'Error: required command not found: %s\n' "$1" >&2
    exit 1
  fi
}

start_profile() {
  local profile="$1"
  local ip="${2:-}"
  local k8s_version="$3"

  local cmd=(
    minikube start
    -p "$profile"
    --driver="$MINIKUBE_DRIVER"
    --network="$NETWORK_NAME"
  )

  if [[ -n "$ip" ]]; then
    cmd+=(--static-ip="$ip")
  fi

  if [[ -n "$MINIKUBE_CPUS" ]]; then
    cmd+=(--cpus="$MINIKUBE_CPUS")
  fi

  if [[ -n "$MINIKUBE_MEMORY" ]]; then
    cmd+=(--memory="$MINIKUBE_MEMORY")
  fi

  if [[ -n "$k8s_version" ]]; then
    cmd+=(--kubernetes-version="$k8s_version")
  fi

  log "Starting profile=${profile} network=${NETWORK_NAME} static-ip=${ip:-auto} cpus=${MINIKUBE_CPUS:-default} memory=${MINIKUBE_MEMORY:-default}"
  "${cmd[@]}"
}

require_cmd docker
require_cmd minikube
require_cmd kubectl

if [[ "$RESET_PROFILES" == "true" ]]; then
  log "Deleting existing minikube profiles: ${SRC_PROFILE}, ${TGT_PROFILE}"
  minikube delete -p "$SRC_PROFILE" >/dev/null 2>&1 || true
  minikube delete -p "$TGT_PROFILE" >/dev/null 2>&1 || true
fi

if docker network inspect "$NETWORK_NAME" >/dev/null 2>&1; then
  if [[ "$RECREATE_NETWORK" == "true" ]]; then
    log "Recreating docker network: ${NETWORK_NAME}"
    docker network rm "$NETWORK_NAME" >/dev/null 2>&1 || true
  else
    log "Docker network already exists: ${NETWORK_NAME}"
  fi
fi

if ! docker network inspect "$NETWORK_NAME" >/dev/null 2>&1; then
  log "Creating docker network: ${NETWORK_NAME}"
  docker network create "$NETWORK_NAME" >/dev/null
fi

network_subnet="$(docker network inspect "$NETWORK_NAME" --format '{{(index .IPAM.Config 0).Subnet}}')"
if [[ -z "$network_subnet" || "$network_subnet" == "<no value>" ]]; then
  printf 'Error: unable to inspect subnet for network %s\n' "$NETWORK_NAME" >&2
  exit 1
fi
log "Docker network subnet: ${network_subnet}"

# Docker only permits static IP assignment on user-configured subnet networks.
# Recreate with the inspected subnet explicitly.
docker network rm "$NETWORK_NAME" >/dev/null 2>&1 || true
docker network create --subnet "$network_subnet" "$NETWORK_NAME" >/dev/null

# First start src without static IP and read the allocated IP.
start_profile "$SRC_PROFILE" "" "$SRC_K8S_VERSION"
SRC_STATIC_IP="$(minikube ip -p "$SRC_PROFILE")"
if [[ -z "$SRC_STATIC_IP" ]]; then
  printf 'Error: unable to determine source profile IP\n' >&2
  exit 1
fi
log "Detected src IP: ${SRC_STATIC_IP}"

# Restart src pinned to the same IP.
minikube delete -p "$SRC_PROFILE" >/dev/null 2>&1 || true
start_profile "$SRC_PROFILE" "$SRC_STATIC_IP" "$SRC_K8S_VERSION"

# Derive tgt by bumping src last octet (e.g. .2 -> .3).
src_prefix="${SRC_STATIC_IP%.*}"
src_last_octet="${SRC_STATIC_IP##*.}"
if ! [[ "$src_last_octet" =~ ^[0-9]+$ ]]; then
  printf 'Error: invalid src IP format: %s\n' "$SRC_STATIC_IP" >&2
  exit 1
fi
tgt_last_octet=$((src_last_octet + 1))
if (( tgt_last_octet > 254 )); then
  printf 'Error: cannot derive tgt IP from src IP %s\n' "$SRC_STATIC_IP" >&2
  exit 1
fi
TGT_STATIC_IP="${src_prefix}.${tgt_last_octet}"
log "Derived tgt IP: ${TGT_STATIC_IP}"

start_profile "$TGT_PROFILE" "$TGT_STATIC_IP" "$TGT_K8S_VERSION"

actual_src_ip="$(minikube ip -p "$SRC_PROFILE")"
actual_tgt_ip="$(minikube ip -p "$TGT_PROFILE")"
if [[ "$actual_src_ip" != "$SRC_STATIC_IP" ]]; then
  printf 'Error: src IP mismatch expected=%s actual=%s\n' "$SRC_STATIC_IP" "$actual_src_ip" >&2
  exit 1
fi
if [[ "$actual_tgt_ip" != "$TGT_STATIC_IP" ]]; then
  printf 'Error: tgt IP mismatch expected=%s actual=%s\n' "$TGT_STATIC_IP" "$actual_tgt_ip" >&2
  exit 1
fi

log "Setting up ingress on target profile: ${TGT_PROFILE}"
minikube addons enable ingress -p "$TGT_PROFILE"
kubectl wait -n ingress-nginx --for=condition=available deployment/ingress-nginx-controller \
  --timeout="$INGRESS_WAIT" --context="$TGT_PROFILE"

controller_args="$(kubectl get deployment ingress-nginx-controller -n ingress-nginx --context="$TGT_PROFILE" -o jsonpath='{.spec.template.spec.containers[0].args}')"
if [[ "$controller_args" != *"--enable-ssl-passthrough"* ]]; then
  log "Enabling SSL passthrough"
  kubectl patch deployment ingress-nginx-controller -n ingress-nginx --context="$TGT_PROFILE" --type='json' \
    -p='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--enable-ssl-passthrough"}]'
fi

https_hostport="$(kubectl get deployment ingress-nginx-controller -n ingress-nginx --context="$TGT_PROFILE" -o jsonpath='{.spec.template.spec.containers[0].ports[?(@.containerPort==443)].hostPort}')"
if [[ "$https_hostport" != "443" ]]; then
  log "Setting hostPort 443"
  if ! kubectl patch deployment ingress-nginx-controller -n ingress-nginx --context="$TGT_PROFILE" --type='json' \
    -p='[{"op":"add","path":"/spec/template/spec/containers/0/ports/1/hostPort","value":443}]' >/dev/null 2>&1; then
    kubectl patch deployment ingress-nginx-controller -n ingress-nginx --context="$TGT_PROFILE" --type='json' \
      -p='[{"op":"add","path":"/spec/template/spec/containers/0/ports/0/hostPort","value":443}]'
  fi
fi

kubectl rollout status deployment/ingress-nginx-controller -n ingress-nginx --context="$TGT_PROFILE" --timeout="$INGRESS_WAIT"

log "Done."
log "Profiles: ${SRC_PROFILE}(${actual_src_ip}) ${TGT_PROFILE}(${actual_tgt_ip})"
