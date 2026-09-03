#!/usr/bin/env bash
# 50 — apply the RemoteCluster + RemoteArbiter CRs and wait for the arbiter
# mon to reach Ready.
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

log "applying RemoteCluster + RemoteArbiter"
kubectl apply -f "${MANIFESTS}/remote-cluster.yaml"
kubectl apply -f "${MANIFESTS}/remote-arbiter.yaml"

log "waiting for RemoteArbiter state=Ready (up to 5m)"
kubectl -n "${OP_NS}" wait remotearbiter/external-arbiter \
  --for=jsonpath='{.status.state}'=Ready --timeout=300s

log "confirming arbiter mon Deployment exists in ${TARGET_NS}"
kubectl -n "${TARGET_NS}" get deploy -l app=rook-ceph-mon -o name | grep -q . \
  || kubectl -n "${TARGET_NS}" get deploy -o name | grep -qi mon \
  || die "no arbiter mon Deployment in ${TARGET_NS}"
log "arbiter deployed"