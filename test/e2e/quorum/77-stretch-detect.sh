# Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
# SPDX-License-Identifier: Apache-2.0
#!/usr/bin/env bash
# 77 — Scenario S8: stretch/external-mode CephCluster silently ignores the arbiter.
#
# S8 in docs/rgw-failure-scenarios.md: Rook's reconcileExternalMons only honours
# CephCluster.Spec.Mon.ExternalMonIDs when the cluster is NOT stretch and NOT
# external (rook .../mon/health.go: the guard returns early otherwise). In those
# modes the operator still deploys the arbiter mon and reserves the id, but Rook
# never integrates it into quorum protection — operators believe they have
# 3-mon resilience that does not exist.
#
# This harness provisions a NON-stretch cluster, so we cannot flip it to stretch
# without a full re-provision. This step is therefore a VALIDATION / guard check:
#
#   1. Assert the cluster under test is non-stretch (precondition for the rest of
#      the quorum harness to be meaningful) — and that the arbiter IS counted
#      (ext-* in quorum). This is the positive confirmation.
#   2. Document the gap: if someone points this operator at a stretch cluster,
#      nothing warns them. We check for (and currently expect the ABSENCE of) any
#      operator-emitted condition/event flagging stretch mode, failing LOUDLY as
#      a known-gap marker so this test starts passing once a guard is added
#      (Recommended Follow-up #2 in the research doc).
#
# Set STRETCH_GUARD_EXPECTED=1 once the operator implements the stretch-mode
# guard; the step will then require the guard to be present.
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

is_stretch() {
  # Rook marks stretch via spec.mon.stretchCluster. external via spec.external.enable.
  local stretch external
  stretch="$(kubectl -n "${ROOK_NS}" get cephcluster my-cluster \
    -o jsonpath='{.spec.mon.stretchCluster}' 2>/dev/null || true)"
  external="$(kubectl -n "${ROOK_NS}" get cephcluster my-cluster \
    -o jsonpath='{.spec.external.enable}' 2>/dev/null || true)"
  [ -n "${stretch}" ] || [ "${external}" = "true" ]
}

log "checking CephCluster mode"
if is_stretch; then
  # If the harness is ever run against a stretch/external cluster, prove the gap
  # directly: the arbiter is deployed+reserved but NOT counted by Rook.
  log "cluster is stretch/external — asserting the arbiter is IGNORED by Rook"
  ids="$(kubectl -n "${ROOK_NS}" get cephcluster my-cluster \
    -o jsonpath='{.spec.mon.externalMonIDs}' 2>/dev/null || echo '[]')"
  Q="$(quorum_json 2>/dev/null || echo '{}')"
  A=$(echo "${Q}" | jq '[.quorum_names[]? | select(startswith("ext-"))] | length')
  log "externalMonIDs=${ids} ; ext-* in quorum=${A}"
  [ "${A}" -eq 0 ] \
    && log "CONFIRMED S8 gap: arbiter reserved but NOT in quorum on a stretch cluster" \
    || die "unexpected: arbiter counted in quorum on a stretch cluster (Rook behaviour changed?)"
  exit 0
fi

# Non-stretch path (this harness): positive confirmation + known-gap marker.
log "cluster is non-stretch (expected for this harness)"
Q="$(quorum_json)"
A=$(echo "${Q}" | jq '[.quorum_names[] | select(startswith("ext-"))] | length')
[ "${A}" -ge 1 ] || die "arbiter not in quorum on a non-stretch cluster — base assumption broken"
log "arbiter is counted in quorum on a non-stretch cluster (as expected)"

# Known-gap marker: look for any operator condition/event mentioning stretch.
guard="$(kubectl -n "${OP_NS}" get remotearbiter external-arbiter \
  -o jsonpath='{range .status.conditions[*]}{.type}{"="}{.message}{"\n"}{end}' 2>/dev/null \
  | grep -iE 'stretch|external.?mode' || true)"

if [ "${STRETCH_GUARD_EXPECTED:-0}" = "1" ]; then
  [ -n "${guard}" ] \
    && { log "PASS S8: operator surfaces a stretch-mode guard: ${guard}"; exit 0; } \
    || die "STRETCH_GUARD_EXPECTED=1 but operator emits no stretch-mode guard condition"
fi

# Default: the guard does NOT exist yet — fail loudly as a tracked known gap so
# this becomes green the moment Recommended Follow-up #2 lands.
[ -z "${guard}" ] \
  && die "KNOWN GAP S8: operator does not warn when pointed at stretch/external clusters (see research doc Follow-up #2). Set STRETCH_GUARD_EXPECTED=1 after adding the guard." \
  || log "PASS S8: stretch-mode guard already present: ${guard}"
