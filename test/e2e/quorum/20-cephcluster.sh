#!/usr/bin/env bash
# 20 — create the source CephCluster (mon.count=3) and wait for HEALTH.
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

log "applying source CephCluster (mon.count=3, loopback OSDs)"
kubectl apply -f "${MANIFESTS}/source-cephcluster.yaml"

log "waiting for CephCluster my-cluster to reach Ready (up to 10m)"
kubectl -n "${ROOK_NS}" wait cephcluster/my-cluster \
  --for=jsonpath='{.status.phase}'=Ready --timeout=600s

log "waiting for 3 mons"
for _ in $(seq 1 60); do
  n=$(kubectl -n "${ROOK_NS}" get deploy -l app=rook-ceph-mon -o name | wc -l | tr -d ' ')
  [ "${n}" -ge 3 ] && break
  sleep 10
done
[ "${n:-0}" -ge 3 ] || die "expected 3 source mons, got ${n:-0}"

kubectl -n "${ROOK_NS}" exec "$(toolbox_pod)" -- ceph -s
log "source Ceph cluster healthy with ${n} mons"