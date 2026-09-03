#!/usr/bin/env bash
# 60 — baseline assertion: BEFORE any kill, quorum has 4 mons and the arbiter
# (ext-*) is already a voting member. This is the proof it actually votes.
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

log "baseline quorum check"
Q="$(quorum_json)"
echo "${Q}" | jq -e '.quorum_names | length >= 4' >/dev/null \
  || die "expected >=4 mons in quorum, got: $(echo "${Q}" | jq -c '.quorum_names')"
echo "${Q}" | jq -e '[.quorum_names[] | select(startswith("ext-"))] | length >= 1' >/dev/null \
  || die "arbiter (ext-*) not in quorum: $(echo "${Q}" | jq -c '.quorum_names')"

log "PASS baseline: $(echo "${Q}" | jq -c '.quorum_names') — arbiter is voting"