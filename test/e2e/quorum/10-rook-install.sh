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

# This harness provisions no CSI storage (no RBD/CephFS), so disable CSI: it
# avoids installing csi-operator.yaml (whose Driver CRDs operator.yaml's
# ROOK_USE_CSI_OPERATOR=true would otherwise require) and saves RAM.
log "disabling CSI in the Rook operator config"
kubectl -n "${ROOK_NS}" patch configmap rook-ceph-operator-config --type merge -p '{"data":{
  "ROOK_USE_CSI_OPERATOR":"false",
  "ROOK_CSI_ENABLE_RBD":"false",
  "ROOK_CSI_ENABLE_CEPHFS":"false",
  "ROOK_CSI_ENABLE_NFS":"false"
}}'
kubectl -n "${ROOK_NS}" rollout restart deploy/rook-ceph-operator
kubectl -n "${ROOK_NS}" rollout status deploy/rook-ceph-operator --timeout=300s
log "rook operator ready"