# Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
# SPDX-License-Identifier: Apache-2.0
#!/usr/bin/env bash
# chaos.sh — RGW-chaos extension of the quorum-survival harness.
#
# Where run.sh proves the CONTROL plane (kill a mon, quorum survives because the
# arbiter votes), chaos.sh proves the DATA plane: that realistic arbiter/mon
# fault scenarios (docs/rgw-failure-scenarios.md S1..S10) do NOT interrupt RGW
# read/write/list traffic — or, for the scenarios where interruption is the
# correct outcome, that they interrupt it for the right reason.
#
# It assumes a cluster already brought up by run.sh steps 00..50 (k3d + Rook +
# operator + a voting arbiter). It adds the RGW data plane (25) and runs the
# chaos steps against a live in-cluster S3 traffic oracle.
#
#   # full flow from scratch (brings the cluster up, then runs chaos):
#   FULL=1 bash test/e2e/quorum/chaos.sh
#
#   # against an already-running cluster (KEEP_CLUSTER=1 from a prior run.sh):
#   bash test/e2e/quorum/chaos.sh
#
#   # a single scenario:
#   bash test/e2e/quorum/chaos.sh 74-partition-arbiter
#
#   # the designed-resilience negative control (expect S3 impact):
#   NEGATIVE_CONTROL=1 bash test/e2e/quorum/chaos.sh 74-partition-arbiter
#
# Env:
#   FULL=1            run 00..50 bring-up first (default: assume cluster exists)
#   KEEP_CLUSTER=1    do not tear the k3d cluster down on exit
#   HOLD=<secs>       hold-with-traffic window per scenario (default per step)
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=test/e2e/quorum/lib.sh
source "${HERE}/lib.sh"

# kubectl + jq are always needed. docker is only needed for the from-scratch
# FULL=1 bring-up (building + importing the operator image into k3d). When
# running against an existing cluster (e.g. the Lima VM, no Docker), skip it.
for t in kubectl jq; do need "${t}"; done
[ "${FULL:-0}" = "1" ] && need docker

# Default chaos sequence. Each is independent and restores its own state, so the
# order only matters in that destructive steps (72 teardown, 76 config churn)
# re-apply/restore before the next runs. Mixed order is intentional: a
# zero-impact step (74) between destructive ones acts as a liveness check.
# 77 (stretch-detect) runs last: by default it is a KNOWN-GAP marker that exits
# non-zero until the operator grows a stretch-mode guard, so we do not let it
# abort the earlier data-plane scenarios.
DEFAULT_STEPS=(
  74-partition-arbiter    # S4/S9 — designed resilience: zero S3 impact
  73-restart-storm        # S3    — arbiter flapping: zero S3 impact
  75-stale-mon-host       # S5    — source mon failover: zero S3 impact
  76-config-churn         # S10   — saveMonConfig churn: zero S3 impact
  72-teardown-race        # S2    — arbiter teardown: zero S3 impact (destructive)
  71-bad-public-addr      # S1    — unreachable arbiter addr: EXPECTED S3 impact
  77-stretch-detect       # S8    — validation / known-gap marker (non-fatal)
)

# Steps whose non-zero exit is a documented known gap, not a harness failure:
# reported as WARN so the overall run still completes. Override with
# STRETCH_GUARD_EXPECTED=1 to make 77 a hard requirement.
KNOWN_GAP_STEPS="77-stretch-detect"

WARNINGS=()
run_step() {
  log "CHAOS STEP: $1"
  if bash "${HERE}/$1.sh"; then
    return 0
  fi
  local rc=$?
  if [ "${STRETCH_GUARD_EXPECTED:-0}" != "1" ] \
     && printf '%s\n' ${KNOWN_GAP_STEPS} | grep -qx "$1"; then
    log "WARN: $1 reported a KNOWN GAP (exit ${rc}) — continuing"
    WARNINGS+=("$1")
    return 0
  fi
  die "chaos step $1 failed (exit ${rc})"
}

cleanup() {
  stop_s3_traffic || true
  [ "${KEEP_CLUSTER:-0}" = "1" ] || { [ "${FULL:-0}" = "1" ] && bash "${HERE}/99-teardown.sh" || true; }
}
trap cleanup EXIT

if [ "${FULL:-0}" = "1" ]; then
  for s in 00-k3d-up 10-rook-install 15-osd-loopdev 20-cephcluster \
           30-operator-deploy 40-remote-kubeconfig 50-apply-arbiter 60-assert-baseline; do
    bash "${HERE}/${s}.sh"
  done
fi

# Data plane is always required for chaos; 25 is idempotent.
bash "${HERE}/25-objectstore.sh"

if [ "$#" -ge 1 ]; then
  for s in "$@"; do run_step "${s}"; done
else
  for s in "${DEFAULT_STEPS[@]}"; do run_step "${s}"; done
fi

if [ "${#WARNINGS[@]}" -gt 0 ]; then
  log "CHAOS COMPLETE with known-gap WARNINGS: ${WARNINGS[*]}"
  log "  (these are documented operator gaps, not harness failures — see docs/rgw-failure-scenarios.md)"
else
  log "ALL CHAOS PASS — RGW behaviour under arbiter/mon faults is as designed"
fi
