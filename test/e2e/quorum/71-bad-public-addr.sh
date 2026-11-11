# Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
# SPDX-License-Identifier: Apache-2.0
#!/usr/bin/env bash
# 71 — Scenario S1: the arbiter's advertised --public-addr is load-bearing.
#
# S1 in docs/rgw-failure-scenarios.md: if the arbiter is in the monmap but its
# advertised address is unreachable, it is counted on paper but cannot actually
# vote when needed — defeating the operator's purpose silently.
#
# We cannot trivially give the arbiter a *wrong* ClusterIP (the operator derives
# it), so we make its advertised address UNREACHABLE at the network layer
# (NetworkPolicy dropping the Ceph messenger ports to the arbiter pod), then kill
# one source mon. If the arbiter's address were not load-bearing, quorum would
# still hold; because it IS, quorum drops below majority and RGW traffic breaks.
#
# Expectation: with the arbiter unreachable, killing one of 3 source mons leaves
# only 2 reachable of 4 → no majority → S3 errors EXPECTED (nonzero delta).
# This is the mirror image of 74 (where the arbiter link is lost but all source
# mons survive, so quorum holds and S3 is uninterrupted).
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

POLICY="arbiter-chaos-block-msgr"

restore_all() {
  log "restoring: removing NetworkPolicy ${POLICY} and bringing victim mon back"
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
base_err="$(s3_error_count)"
log "baseline S3 error count: ${base_err}"

# 1) make the arbiter's advertised address unreachable (deny all ingress to the
#    arbiter pod — effectively the same as a bogus/unroutable --public-addr).
log "applying NetworkPolicy ${POLICY}: deny ingress to arbiter pod"
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
  policyTypes: [Ingress]
  ingress: []     # drop all ingress → arbiter unreachable
YAML

log "waiting ${ISOLATE_SETTLE:-25}s for the arbiter to drop from quorum"
sleep "${ISOLATE_SETTLE:-25}"

# 2) kill one source mon. 3 source + arbiter = 4; arbiter unreachable + 1 source
#    down → 2 reachable of 4 → no majority.
MONS=()
while IFS= read -r line; do [ -n "${line}" ] && MONS+=("${line}"); done \
  < <(kubectl -n "${ROOK_NS}" get deploy -l app=rook-ceph-mon -o name | sort)
[ "${#MONS[@]}" -ge 3 ] || die "need >=3 source mons, found ${#MONS[@]}"
mon_id() { basename "$1" | sed 's/^rook-ceph-mon-//'; }
: > "${VICTIM_FILE}"
log "scaling down source mon ${MONS[0]} (ceph mon '$(mon_id "${MONS[0]}")')"
kubectl -n "${ROOK_NS}" scale "${MONS[0]}" --replicas=0
mon_id "${MONS[0]}" >> "${VICTIM_FILE}"

log "holding ${HOLD:-60}s with traffic while quorum is below majority"
hold_with_traffic "${HOLD:-60}"

# With the arbiter unreachable AND a source mon down, quorum must be lost.
# Confirm the control plane first (best-effort: ceph may block), then assert the
# data-plane impact, which is the actual thing we care about.
if out="$(ceph_exec ceph -s 2>&1)"; then
  Q="$(quorum_json 2>/dev/null || echo '{}')"
  N=$(echo "${Q}" | jq '.quorum_names | length // 0')
  log "ceph reachable; ${N} mons in quorum (expected < majority)"
else
  log "ceph could not reach quorum (expected — majority lost)"
fi

# The core assertion: RGW traffic WAS interrupted, proving the arbiter's
# advertised address is load-bearing (a healthy-looking but unreachable arbiter
# does not protect S3).
assert_s3_delta "${base_err}" nonzero
log "PASS S1: unreachable arbiter address → quorum lost on single mon kill → RGW interrupted"
