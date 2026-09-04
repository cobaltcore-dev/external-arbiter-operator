#!/usr/bin/env bash
# 70 — fault injection: scale one SOURCE mon Deployment to 0 (a scaled-down mon
# gives a more stable window than a delete Rook races to recreate).
# With NEGATIVE_CONTROL=1, kill a second source mon too (2/4 → no quorum).
#
# Records the victim ceph mon id(s) to VICTIM_FILE so 80-assert-survival.sh can
# assert they are actually absent from quorum at assertion time (not merely that
# >=3 mons remain, which Rook re-creating the victim would also satisfy).
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# bash 3.2 (macOS /bin/bash) has no mapfile — use a portable read loop.
MONS=()
while IFS= read -r line; do [ -n "${line}" ] && MONS+=("${line}"); done \
  < <(kubectl -n "${ROOK_NS}" get deploy -l app=rook-ceph-mon -o name | sort)
[ "${#MONS[@]}" -ge 3 ] || die "need >=3 source mons to kill one, found ${#MONS[@]}"

# Rook mon Deployment is rook-ceph-mon-<id>; the ceph mon name is that <id>.
mon_id() { basename "$1" | sed 's/^rook-ceph-mon-//'; }

: > "${VICTIM_FILE}"
VICTIM="${MONS[0]}"
log "scaling down source mon ${VICTIM} (ceph mon '$(mon_id "${VICTIM}")')"
kubectl -n "${ROOK_NS}" scale "${VICTIM}" --replicas=0
mon_id "${VICTIM}" >> "${VICTIM_FILE}"

if [ "${NEGATIVE_CONTROL:-0}" = "1" ]; then
  log "NEGATIVE_CONTROL: also scaling down ${MONS[1]} (ceph mon '$(mon_id "${MONS[1]}")')"
  kubectl -n "${ROOK_NS}" scale "${MONS[1]}" --replicas=0
  mon_id "${MONS[1]}" >> "${VICTIM_FILE}"
fi

log "waiting ${KILL_SETTLE:-20}s for mon election to settle"
sleep "${KILL_SETTLE:-20}"