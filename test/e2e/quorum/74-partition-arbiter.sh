# Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
# SPDX-License-Identifier: Apache-2.0
#!/usr/bin/env bash
# 74 — Scenario S4/S9: cross-cluster partition isolating the arbiter.
#
# The DESIGNED resilience: losing only the arbiter link must NOT disrupt RGW,
# because the 3 source mons still form a majority (3/4). This is the property
# the operator exists to provide — the arbiter must not become a new single
# point of failure for the data path.
#
# Injection: NetworkPolicy dropping the Ceph messenger ports (3300/6789) to the
# arbiter pod — the same effect as the inter-cluster link failing (S4) or the
# NodePort messenger ports being firewalled (S9).
#
# Expectation:
#   default:            arbiter drops from quorum, 3/4 source mons remain in
#                       quorum, S3 UNINTERRUPTED (zero error delta).
#   NEGATIVE_CONTROL=1: also kill one source mon → 2/4 reachable → majority lost
#                       → S3 errors EXPECTED (proves the zero-error result above
#                       is not vacuous).
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

POLICY="arbiter-chaos-partition"

restore_all() {
  log "restoring: removing NetworkPolicy ${POLICY}; bringing any victim mon back"
  kubectl -n "${TARGET_NS}" delete networkpolicy "${POLICY}" --ignore-not-found >/dev/null 2>&1 || true
  if [ -s "${VICTIM_FILE}" ]; then
    while IFS= read -r v; do
      [ -n "${v}" ] || continue
      kubectl -n "${ROOK_NS}" scale "deploy/rook-ceph-mon-${v}" --replicas=1 >/dev/null 2>&1 || true
    done < "${VICTIM_FILE}"
  fi
  stop_s3_traffic
}
trap restore_all EXIT INT TERM

start_s3_traffic
base_err="$(s3_error_count)"; base_res="$(s3_result_count)"
log "baseline S3 error count: ${base_err} (results so far: ${base_res})"

# Partition the arbiter: drop the messenger ports 3300 (msgr2) + 6789 (msgr1).
# We drop both ingress and egress on those ports so the arbiter can neither be
# reached by, nor reach, its peers — a faithful link partition.
log "applying NetworkPolicy ${POLICY}: block msgr 3300/6789 to/from arbiter"
kubectl apply -f - <<YAML
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: ${POLICY}
  namespace: ${TARGET_NS}
spec:
  podSelector:
    matchLabels:
      ceph.cobaltcore.sap.com/lookup: external-arbiter
  policyTypes: [Ingress, Egress]
  # Empty ingress + egress with the messenger ports is simplest expressed as a
  # full deny; the arbiter pod has no other traffic it must serve in this lab.
  ingress: []
  egress: []
YAML

: > "${VICTIM_FILE}"
if [ "${NEGATIVE_CONTROL:-0}" = "1" ]; then
  MONS=()
  while IFS= read -r line; do [ -n "${line}" ] && MONS+=("${line}"); done \
    < <(kubectl -n "${ROOK_NS}" get deploy -l app=rook-ceph-mon -o name | sort)
  [ "${#MONS[@]}" -ge 3 ] || die "need >=3 source mons, found ${#MONS[@]}"
  mon_id() { basename "$1" | sed 's/^rook-ceph-mon-//'; }
  log "NEGATIVE_CONTROL: also scaling down source mon ${MONS[0]}"
  kubectl -n "${ROOK_NS}" scale "${MONS[0]}" --replicas=0
  mon_id "${MONS[0]}" >> "${VICTIM_FILE}"
fi

log "waiting ${ISOLATE_SETTLE:-25}s for the mon map to react"
sleep "${ISOLATE_SETTLE:-25}"
log "holding ${HOLD:-60}s with traffic"
hold_with_traffic "${HOLD:-60}"

if [ "${NEGATIVE_CONTROL:-0}" = "1" ]; then
  # arbiter partitioned + 1 source mon down = 2/4 reachable → majority lost.
  assert_s3_delta "${base_err}" nonzero
  log "PASS negative control S4: arbiter link lost + 1 mon down → RGW interrupted"
  exit 0
fi

# Designed case: only the arbiter is partitioned; 3 source mons still form
# majority. Confirm the control plane shows a healthy source-mon quorum, then
# assert the data plane was UNINTERRUPTED.
Q="$(quorum_json)"
N=$(echo "${Q}" | jq '.quorum_names | length')
# 3 source mons must still be in quorum; the arbiter may or may not have been
# evicted yet, so accept >= 3 (majority of 4) rather than an exact count.
[ "${N}" -ge 3 ] || die "expected majority quorum (>=3) with only arbiter lost, got ${N}: $(echo "${Q}" | jq -c '.quorum_names')"
log "control plane healthy: ${N} mons in quorum with arbiter partitioned"

assert_s3_delta "${base_err}" zero "${base_res}"
log "PASS S4/S9: arbiter link lost, source quorum holds, RGW uninterrupted"
