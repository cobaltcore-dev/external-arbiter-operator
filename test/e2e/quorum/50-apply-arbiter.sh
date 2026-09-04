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

# state=Ready above is the real gate. Confirm the arbiter Deployment landed in
# the target ns by the operator's own label (the arbiter is NOT labelled
# app=rook-ceph-mon — makeDeploymentSpec rewrites the labels to lookup=<name>).
log "confirming arbiter Deployment exists in ${TARGET_NS}"
kubectl -n "${TARGET_NS}" get deploy \
  -l ceph.cobaltcore.sap.com/lookup=external-arbiter -o name | grep -q . \
  || die "no arbiter Deployment in ${TARGET_NS}"
log "arbiter deployed"