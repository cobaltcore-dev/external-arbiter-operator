#!/usr/bin/env bash
# 30 — build the operator image, import into k3d, install via helm (local.yaml).
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

need docker
need helm

log "building operator image ${IMAGE}"
docker build -t "${IMAGE}" -f "${REPO_ROOT}/Dockerfile" "${REPO_ROOT}"

log "importing image into k3d cluster ${CLUSTER}"
k3d image import "${IMAGE}" -c "${CLUSTER}"

log "helm install operator into ${OP_NS} (pullPolicy Never via local.yaml)"
helm upgrade --install \
  --create-namespace --namespace "${OP_NS}" \
  --values "${CHART}/local.yaml" \
  arbiter-operator "${CHART}"

kubectl -n "${OP_NS}" rollout status deploy -l app.kubernetes.io/instance=arbiter-operator --timeout=180s \
  || kubectl -n "${OP_NS}" rollout status deploy --timeout=180s
log "operator deployed"