#!/usr/bin/env bash
# 70 — fault injection: scale one SOURCE mon Deployment to 0 (a scaled-down mon
# gives a more stable window than a delete Rook races to recreate).
# With NEGATIVE_CONTROL=1, kill a second source mon too (2/4 → no quorum).
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

mapfile -t MONS < <(kubectl -n "${ROOK_NS}" get deploy -l app=rook-ceph-mon -o name)
[ "${#MONS[@]}" -ge 3 ] || die "need >=3 source mons to kill one, found ${#MONS[@]}"

VICTIM="${MONS[0]}"
log "scaling down source mon ${VICTIM}"
kubectl -n "${ROOK_NS}" scale "${VICTIM}" --replicas=0

if [ "${NEGATIVE_CONTROL:-0}" = "1" ]; then
  log "NEGATIVE_CONTROL: also scaling down ${MONS[1]}"
  kubectl -n "${ROOK_NS}" scale "${MONS[1]}" --replicas=0
fi

log "waiting ${KILL_SETTLE:-20}s for mon election to settle"
sleep "${KILL_SETTLE:-20}"