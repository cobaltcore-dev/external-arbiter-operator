#!/usr/bin/env bash
# 99 — teardown. Deletes the k3d cluster (KEEP_CLUSTER=1 to keep it for debug).
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

if [ "${KEEP_CLUSTER:-0}" = "1" ]; then
  log "KEEP_CLUSTER=1 — leaving ${CLUSTER} running"
  exit 0
fi
if k3d cluster list -o json | jq -e --arg n "${CLUSTER}" '.[] | select(.name==$n)' >/dev/null 2>&1; then
  log "deleting k3d cluster ${CLUSTER}"
  k3d cluster delete "${CLUSTER}"
fi