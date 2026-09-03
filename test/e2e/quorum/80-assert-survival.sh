#!/usr/bin/env bash
# 80 — survival assertion. Default: quorum survived (>=3 mons, arbiter still
# voting, not HEALTH_ERR/no-quorum). NEGATIVE_CONTROL=1 inverts: expect quorum
# LOST (2/4), proving the positive assertion isn't vacuous.
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

if [ "${NEGATIVE_CONTROL:-0}" = "1" ]; then
  log "negative control: expecting quorum LOST"
  if kubectl -n "${ROOK_NS}" exec "$(toolbox_pod)" -- ceph -s 2>&1 | grep -qiE 'HEALTH_ERR|no quorum|quorum .* down'; then
    log "PASS negative control: quorum lost at 2/4 as expected"
    exit 0
  fi
  Q="$(quorum_json 2>/dev/null || echo '{}')"
  N=$(echo "${Q}" | jq '.quorum_names | length // 0')
  [ "${N}" -lt 3 ] && { log "PASS negative control: only ${N} mons in quorum"; exit 0; }
  die "negative control expected lost quorum, but ${N} mons still in quorum"
fi

log "survival assertion"
kubectl -n "${ROOK_NS}" exec "$(toolbox_pod)" -- ceph -s | grep -qiE 'HEALTH_ERR|no quorum' \
  && die "Ceph reports HEALTH_ERR / no quorum after one mon loss"

Q="$(quorum_json)"
N=$(echo "${Q}" | jq '.quorum_names | length')
A=$(echo "${Q}" | jq '[.quorum_names[] | select(startswith("ext-"))] | length')
[ "${N}" -ge 3 ] || die "quorum shrank below 3: $(echo "${Q}" | jq -c '.quorum_names')"
[ "${A}" -ge 1 ] || die "arbiter dropped out of quorum: $(echo "${Q}" | jq -c '.quorum_names')"

log "PASS: quorum survived one mon loss (${N} mons, arbiter voting) — property proven"