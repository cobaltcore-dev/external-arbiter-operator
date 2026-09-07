#!/usr/bin/env bash
# 00 — bring up the k3d cluster (1 server + 2 agents for mon anti-affinity).
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

need k3d
need kubectl

if k3d cluster list -o json | jq -e --arg n "${CLUSTER}" '.[] | select(.name==$n)' >/dev/null 2>&1; then
  log "k3d cluster ${CLUSTER} already exists — reusing"
else
  log "creating k3d cluster ${CLUSTER} (3 nodes)"
  k3d cluster create "${CLUSTER}" \
    --agents 2 \
    --k3s-arg "--disable=traefik@server:0" \
    --k3s-arg "--disable=metrics-server@server:0" \
    --wait
fi

kubectl cluster-info >/dev/null || die "cluster not reachable"
kubectl get nodes
log "k3d up"