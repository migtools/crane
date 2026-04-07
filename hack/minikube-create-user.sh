#!/usr/bin/env bash
set -euo pipefail

# Create a cert-based Kubernetes user for a Minikube profile/context.
#
# Example:
#   ./hack/minikube-create-user.sh --profile src --user dev
#
# Optional:
#   --context src-dev
#   --org developers
#   --days 365
#   --output-dir /tmp/minikube-users
#
# Notes:
# - This script signs a client cert using Minikube's CA key.
# - It then updates kubeconfig with:
#   - user entry key: <context>
#   - context: <context> (cluster from --profile context, user=<context>)

usage() {
  cat <<'EOF'
Usage:
  minikube-create-user.sh --profile <context-name> --user <username> [options]

Required:
  --profile <name>   Existing kubeconfig context/profile (e.g. src, tgt)
  --user <name>      Username to create (e.g. dev)

Optional:
  --context <name>   New context name (default: <profile>-<user>)
  --org <name>       User organization for cert subject (default: developers)
  --days <n>         Certificate validity in days (default: 365)
  --output-dir <dir> Output directory for key/csr/crt files
                     (default: ~/.minikube/users/<profile>/<user>)
  --help             Show this help
EOF
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf 'Error: required command not found: %s\n' "$1" >&2
    exit 1
  fi
}

PROFILE=""
USER_NAME=""
CONTEXT_NAME=""
ORG_NAME="developers"
DAYS="365"
OUTPUT_DIR=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --profile)
      PROFILE="${2:-}"
      shift 2
      ;;
    --user)
      USER_NAME="${2:-}"
      shift 2
      ;;
    --context)
      CONTEXT_NAME="${2:-}"
      shift 2
      ;;
    --org)
      ORG_NAME="${2:-}"
      shift 2
      ;;
    --days)
      DAYS="${2:-}"
      shift 2
      ;;
    --output-dir)
      OUTPUT_DIR="${2:-}"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      printf 'Error: unknown argument: %s\n' "$1" >&2
      usage
      exit 1
      ;;
  esac
done

if [[ -z "$PROFILE" || -z "$USER_NAME" ]]; then
  printf 'Error: --profile and --user are required\n' >&2
  usage
  exit 1
fi

if [[ -z "$CONTEXT_NAME" ]]; then
  CONTEXT_NAME="${PROFILE}-${USER_NAME}"
fi

if ! [[ "$DAYS" =~ ^[0-9]+$ ]]; then
  printf 'Error: --days must be a positive integer\n' >&2
  exit 1
fi

MINIKUBE_HOME="${MINIKUBE_HOME:-$HOME/.minikube}"
CA_CERT="${MINIKUBE_HOME}/ca.crt"
CA_KEY="${MINIKUBE_HOME}/ca.key"

if [[ -z "$OUTPUT_DIR" ]]; then
  OUTPUT_DIR="${MINIKUBE_HOME}/users/${PROFILE}/${USER_NAME}"
fi

require_cmd kubectl
require_cmd openssl

if [[ ! -f "$CA_CERT" ]]; then
  printf 'Error: CA cert not found: %s\n' "$CA_CERT" >&2
  exit 1
fi
if [[ ! -f "$CA_KEY" ]]; then
  printf 'Error: CA key not found: %s\n' "$CA_KEY" >&2
  exit 1
fi

if ! kubectl config get-contexts "$PROFILE" >/dev/null 2>&1; then
  printf 'Error: kubeconfig context/profile not found: %s\n' "$PROFILE" >&2
  exit 1
fi

mkdir -p "$OUTPUT_DIR"

USER_KEY="${OUTPUT_DIR}/${USER_NAME}.key"
USER_CSR="${OUTPUT_DIR}/${USER_NAME}.csr"
USER_CRT="${OUTPUT_DIR}/${USER_NAME}.crt"

openssl genrsa -out "$USER_KEY" 2048 >/dev/null 2>&1
chmod 600 "$USER_KEY"
openssl req -new -key "$USER_KEY" -out "$USER_CSR" -subj "/CN=${USER_NAME}/O=${ORG_NAME}" >/dev/null 2>&1
openssl x509 -req \
  -in "$USER_CSR" \
  -CA "$CA_CERT" \
  -CAkey "$CA_KEY" \
  -CAcreateserial \
  -out "$USER_CRT" \
  -days "$DAYS" >/dev/null 2>&1

CLUSTER_NAME="$(kubectl config view -o jsonpath="{.contexts[?(@.name==\"$PROFILE\")].context.cluster}")"
if [[ -z "$CLUSTER_NAME" ]]; then
  printf 'Error: unable to resolve cluster name for context: %s\n' "$PROFILE" >&2
  exit 1
fi

kubectl config set-credentials "$USER_NAME" \
  --client-certificate="$USER_CRT" \
  --client-key="$USER_KEY" \
  --embed-certs=true >/dev/null

kubectl config set-context "$CONTEXT_NAME" \
  --cluster="$CLUSTER_NAME" \
  --user="$CONTEXT_NAME" >/dev/null

printf 'Created user and context successfully.\n'
printf '  profile/context source: %s\n' "$PROFILE"
printf '  user: %s\n' "$USER_NAME"
printf '  context: %s\n' "$CONTEXT_NAME"
printf '  cert/key dir: %s\n' "$OUTPUT_DIR"
printf '\n'
printf 'Next step (namespace-scoped access):\n'
printf '  kubectl --context %s -n <namespace> create rolebinding %s-admin --clusterrole=admin --user=%s\n' "$PROFILE" "$USER_NAME" "$USER_NAME"
