#!/usr/bin/env bash
# 10 — install cert-manager (operator webhooks need it) + the Rook operator.
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

log "installing cert-manager ${CERT_MANAGER_VERSION}"
kubectl apply -f "https://github.com/cert-manager/cert-manager/releases/download/${CERT_MANAGER_VERSION}/cert-manager.yaml"
kubectl -n cert-manager rollout status deploy/cert-manager-webhook --timeout=180s

log "installing Rook operator v${ROOK_VERSION}"
for f in crds.yaml common.yaml operator.yaml toolbox.yaml; do
  kubectl apply -f "$(rook_url "${f}")"
done
kubectl -n "${ROOK_NS}" rollout status deploy/rook-ceph-operator --timeout=300s
log "rook operator ready"