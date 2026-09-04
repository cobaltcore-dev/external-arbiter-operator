#!/usr/bin/env bash
# 80 — survival assertion.
#   default:            quorum survived — the victim mon is gone AND the arbiter
#                       still votes (3 mons remain: 2 source + arbiter).
#   NEGATIVE_CONTROL=1: quorum LOST (2/4) — proves the positive assertion isn't
#                       vacuous. A lost-quorum ceph call fails/times out; we
#                       treat that (or an explicit health string) as PASS.
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

if [ "${NEGATIVE_CONTROL:-0}" = "1" ]; then
  log "negative control: expecting quorum LOST"
  # A bare non-zero from ceph is NOT proof of lost quorum — the toolbox could be
  # unready, or kubectl exec could blip. Require the toolbox to be reachable
  # first, then prove loss positively: either ceph blocks until --connect-timeout
  # (exit 124 from the host timeout, or a non-zero from --connect-timeout with
  # the toolbox otherwise healthy), or quorum_status reports < majority.
  ceph_exec ceph version >/dev/null 2>&1 || die "toolbox not reachable — cannot run negative control"

  if ! out="$(ceph_exec ceph -s 2>&1)"; then
    log "PASS negative control: ceph could not reach quorum (timed out) — quorum lost"
    exit 0
  fi
  if echo "${out}" | grep -qiE 'no quorum|quorum .* down|mon .* down|[0-9]+/[0-9]+ mons down'; then
    log "PASS negative control: ceph reports lost quorum"
    exit 0
  fi
  # Still reachable: count survivors; <3 of 4 means no majority.
  Q="$(quorum_json 2>/dev/null || echo '{}')"
  N=$(echo "${Q}" | jq '.quorum_names | length // 0')
  [ "${N}" -lt 3 ] && { log "PASS negative control: only ${N} mons in quorum"; exit 0; }
  die "negative control expected lost quorum, but ${N} mons still in quorum"
fi

log "survival assertion"
[ -s "${VICTIM_FILE}" ] || die "no victim recorded by 70-kill-mon.sh (${VICTIM_FILE})"

Q="$(quorum_json)"
NAMES="$(echo "${Q}" | jq -r '.quorum_names[]')"
N=$(echo "${Q}" | jq '.quorum_names | length')
A=$(echo "${Q}" | jq '[.quorum_names[] | select(startswith("ext-"))] | length')

# The kill must be effective AT ASSERTION TIME: each victim must be absent from
# quorum. This is what stops a false pass when Rook re-creates the scaled-down
# mon before we look (>=3 alone would be satisfied by the victim coming back).
while IFS= read -r v; do
  [ -n "${v}" ] || continue
  if echo "${NAMES}" | grep -qx "${v}"; then
    die "victim mon '${v}' is back in quorum — kill was not effective: ${NAMES//$'\n'/ }"
  fi
done < "${VICTIM_FILE}"

# Quorum retained with the arbiter as a voting member. Exactly 3 mons must
# remain (3 source − 1 killed + arbiter); a 4th means Rook recreated the victim
# under a new id and the arbiter's vote was no longer load-bearing — fail loudly.
[ "${N}" -eq 3 ] || die "expected exactly 3 mons in quorum, got ${N}: ${NAMES//$'\n'/ }"
[ "${A}" -ge 1 ] || die "arbiter dropped out of quorum: ${NAMES//$'\n'/ }"

log "PASS: victim absent, ${N} mons in quorum incl. arbiter — property proven"