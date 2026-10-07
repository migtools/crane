#!/usr/bin/env bash
# Run crane export and sanity-check output (Phase C + checklist). Requires cluster access.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
NS="${CRANE_EXPORT_NS:-crane-complex-demo}"
EXPORT_DIR="${1:-${SCRIPT_DIR}/export-crd-test-out}"

CRANE_BIN="${CRANE_BIN:-}"
if [[ -z "${CRANE_BIN}" ]]; then
  if [[ -x "${REPO_ROOT}/crane" ]]; then
    CRANE_BIN="${REPO_ROOT}/crane"
  else
    echo "Building crane into repo root..."
    (cd "${REPO_ROOT}" && go build -o "${REPO_ROOT}/crane" .)
    CRANE_BIN="${REPO_ROOT}/crane"
  fi
fi

rm -rf "${EXPORT_DIR}"
mkdir -p "${EXPORT_DIR}"

echo "Running: ${CRANE_BIN} export --export-dir ${EXPORT_DIR} -n ${NS} --cluster-scoped-rbac"
"${CRANE_BIN}" export --export-dir "${EXPORT_DIR}" -n "${NS}" --cluster-scoped-rbac

RES="${EXPORT_DIR}/resources/${NS}"
CLUSTER="${RES}/_cluster"

die() { echo "VERIFY FAIL: $*" >&2; exit 1; }

[[ -d "${CLUSTER}" ]] || die "_cluster missing: ${CLUSTER}"

# Namespaced workload + CR (not only CRs)
shopt -s nullglob
ns_files=("${RES}"/*.yaml)
[[ ${#ns_files[@]} -gt 0 ]] || die "no YAML in ${RES}"
ok_ns=0
for k in Deployment Service ConfigMap ServiceAccount Widget Secret; do
  for f in "${ns_files[@]}"; do
    grep -q "kind: ${k}" "$f" 2>/dev/null && ok_ns=1 && break 2
  done
done
[[ "${ok_ns}" -eq 1 ]] || die "expected namespaced Deployment/Service/ConfigMap/SA/Widget/Secret in ${RES}"
shopt -u nullglob

# CRD + cluster RBAC in _cluster
shopt -s nullglob
cr_files=("${CLUSTER}"/*.yaml)
[[ ${#cr_files[@]} -gt 0 ]] || die "no YAML files in _cluster"
grep -l "kind: CustomResourceDefinition" "${cr_files[@]}" &>/dev/null || die "no CustomResourceDefinition in _cluster"
grep -q "name: widgets.stable.example.com" "${cr_files[@]}" || die "CRD metadata.name widgets.stable.example.com not found in _cluster YAML"
# Filenames follow getFilePath: <Kind>_<group>_<version>_clusterscoped_<name>.yaml
ls "${CLUSTER}"/ClusterRole_*.yaml &>/dev/null || die "no ClusterRole_* file in _cluster"
ls "${CLUSTER}"/ClusterRoleBinding_*.yaml &>/dev/null || die "no ClusterRoleBinding_* file in _cluster"
shopt -u nullglob

echo "VERIFY OK: namespaced resources + CRD + ClusterRole/ClusterRoleBinding under ${EXPORT_DIR}"
