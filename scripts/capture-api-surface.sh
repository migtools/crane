#!/bin/bash
# capture-api-surface.sh
# Run this on a machine with kubectl access to the target cluster.
# It captures the full API surface (all served versions, not just preferred).
# Pass the output file to: crane validate --api-resources api-surface.json
#
# Usage:
#   bash capture-api-surface.sh --context <context-name> -o <output-file>
#   bash capture-api-surface.sh --kubeconfig <path> -o <output-file>
#   bash capture-api-surface.sh --kubeconfig <path> --context <context-name> -o <output-file>
#
# Examples:
#   bash capture-api-surface.sh --context my-target-cluster -o api-surface.json
#   bash capture-api-surface.sh --kubeconfig /path/to/kubeconfig -o api-surface.json

set -euo pipefail

CONTEXT=""
KUBECONFIG_PATH=""
OUTPUT=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --context)    CONTEXT="$2"; shift 2 ;;
    --kubeconfig) KUBECONFIG_PATH="$2"; shift 2 ;;
    -o)           OUTPUT="$2"; shift 2 ;;
    *)            echo "Unknown flag: $1"; exit 1 ;;
  esac
done

if [ -z "$OUTPUT" ]; then
  echo "Usage: bash capture-api-surface.sh [--context <name>] [--kubeconfig <path>] -o <output-file>"
  exit 1
fi

# Build kubectl flags
KUBECTL_FLAGS=""
[ -n "$CONTEXT" ] && KUBECTL_FLAGS="$KUBECTL_FLAGS --context=$CONTEXT"
[ -n "$KUBECONFIG_PATH" ] && KUBECTL_FLAGS="$KUBECTL_FLAGS --kubeconfig=$KUBECONFIG_PATH"

echo "Capturing API surface..."
[ -n "$CONTEXT" ] && echo "  Context: $CONTEXT"
[ -n "$KUBECONFIG_PATH" ] && echo "  Kubeconfig: $KUBECONFIG_PATH"

# Step 1: Get all API versions served by the cluster
API_VERSIONS=$(kubectl api-versions $KUBECTL_FLAGS)

# Step 2: For each API version, fetch the list of resources it serves
echo '{"apiResourceLists":[' > "$OUTPUT"
FIRST=true

for GV in $API_VERSIONS; do
  # Core API (v1) lives at /api/v1, everything else at /apis/<group>/<version>
  if [ "$GV" = "v1" ]; then
    ENDPOINT="/api/v1"
  else
    ENDPOINT="/apis/$GV"
  fi

  RESOURCES=$(kubectl get --raw "$ENDPOINT" $KUBECTL_FLAGS 2>/dev/null) || continue

  if [ "$FIRST" = true ]; then
    FIRST=false
  else
    echo "," >> "$OUTPUT"
  fi
  echo "$RESOURCES" >> "$OUTPUT"
done

echo ']}' >> "$OUTPUT"

COUNT=$(echo "$API_VERSIONS" | wc -l | tr -d ' ')
echo "Done. Captured $COUNT API versions to $OUTPUT"
