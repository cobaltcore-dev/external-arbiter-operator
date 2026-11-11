# Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
# SPDX-License-Identifier: Apache-2.0
#!/usr/bin/env bash
# 25 — stand up the RGW data plane (CephObjectStore + CephObjectStoreUser) so
# the chaos steps (71..77) have live S3 traffic to measure. Not a test itself:
# it is the data-plane analogue of 15-osd-loopdev / 20-cephcluster bring-up.
#
# Only runs as part of the RGW-chaos flow (chaos.sh) — the base quorum run.sh
# does not need it. Idempotent: re-apply is harmless.
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# Idempotent fast-path: if the store is already Ready AND the S3 user Secret
# exists (e.g. an already-provisioned cluster like the Lima VM from the README
# quick-start), skip the apply entirely — re-applying this manifest's subset
# spec over a store Rook already created can trigger an unwanted reconcile.
if kubectl -n "${ROOK_NS}" get "cephobjectstore/${RGW_STORE}" \
     -o jsonpath='{.status.phase}' 2>/dev/null | grep -qx Ready \
   && kubectl -n "${ROOK_NS}" get secret "${RGW_SECRET}" >/dev/null 2>&1; then
  log "RGW ${RGW_STORE} already Ready and ${RGW_SECRET} exists — skipping apply"
  exit 0
fi

log "applying CephObjectStore + CephObjectStoreUser (${RGW_STORE}/${RGW_USER})"
kubectl apply -f "${MANIFESTS}/object-store.yaml"

log "waiting for CephObjectStore ${RGW_STORE} to reach Ready (up to 5m)"
kubectl -n "${ROOK_NS}" wait "cephobjectstore/${RGW_STORE}" \
  --for=jsonpath='{.status.phase}'=Ready --timeout=300s \
  || die "CephObjectStore ${RGW_STORE} did not reach Ready"

# The RGW deployment must actually have a ready replica — a Ready phase on the
# CR can briefly precede the gateway pod being servable.
log "waiting for RGW gateway deployment to have a ready replica"
for _ in $(seq 1 30); do
  ready="$(kubectl -n "${ROOK_NS}" get deploy \
    -l "app=rook-ceph-rgw,rook_object_store=${RGW_STORE}" \
    -o jsonpath='{.items[*].status.readyReplicas}' 2>/dev/null || true)"
  [ "${ready:-0}" -ge 1 ] 2>/dev/null && break
  sleep 10
done
[ "${ready:-0}" -ge 1 ] 2>/dev/null || die "RGW gateway has no ready replica"

# arbiter-s3-bench reads creds from this Secret; Rook creates it after the user
# reconciles. Wait for it so the first traffic cycle does not fail on missing
# credentials.
log "waiting for S3 credentials Secret ${RGW_SECRET}"
for _ in $(seq 1 30); do
  kubectl -n "${ROOK_NS}" get secret "${RGW_SECRET}" >/dev/null 2>&1 && break
  sleep 5
done
kubectl -n "${ROOK_NS}" get secret "${RGW_SECRET}" >/dev/null 2>&1 \
  || die "S3 credentials Secret ${RGW_SECRET} never appeared"

log "RGW data plane ready: store=${RGW_STORE} user=${RGW_USER} secret=${RGW_SECRET}"
