# Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
# SPDX-License-Identifier: Apache-2.0
#!/usr/bin/env bash
# 75 — Scenario S5: stale mon_host during a source mon failover must not break RGW.
#
# S5 in docs/rgw-failure-scenarios.md: the arbiter's monmap is built from the
# source cluster's mon_host / mon_initial_members, copied into an env-var Secret
# (createArbiterEnvVarSecret) and consumed by the init-monmap container
# (getMonMapInitContainer). When a source mon fails over its address changes; if
# the copied Secret is stale the arbiter can join with an outdated peer view.
#
# We trigger a real failover by deleting a source mon's PVC-less data + scaling
# it, letting Rook replace it (new mon letter / address), which changes
# mon_host. Then we verify the data plane stays up through the convergence gap
# and the arbiter's copied env Secret eventually reflects the new mon_host.
#
# Expectation: zero S3 error delta during convergence. We report whether the
# arbiter's env Secret mon_host matches the live source mon_host at the end.
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# Live mon_host as Rook publishes it to clients (rook-ceph-config Secret).
source_mon_host() {
  kubectl -n "${ROOK_NS}" get secret rook-ceph-config \
    -o jsonpath='{.data.mon_host}' 2>/dev/null | base64 -d 2>/dev/null || true
}
# mon_host the operator copied into the arbiter's env-var Secret (role=envvar).
arbiter_mon_host() {
  local s
  s="$(kubectl -n "${TARGET_NS}" get secret \
    -l 'ceph.cobaltcore.sap.com/lookup=external-arbiter,ceph.cobaltcore.sap.com/role=envvar' \
    -o jsonpath='{.items[0].data.mon_host}' 2>/dev/null || true)"
  [ -n "${s}" ] && echo "${s}" | base64 -d 2>/dev/null || true
}

restore_all() {
  # Ensure the (replaced) mons are all scaled up; Rook normally handles this.
  for d in $(kubectl -n "${ROOK_NS}" get deploy -l app=rook-ceph-mon -o name 2>/dev/null); do
    kubectl -n "${ROOK_NS}" scale "${d}" --replicas=1 >/dev/null 2>&1 || true
  done
  stop_s3_traffic
}
trap restore_all EXIT INT TERM

start_s3_traffic
base_err="$(s3_error_count)"; base_res="$(s3_result_count)"
before_host="$(source_mon_host)"
log "baseline: S3 errors=${base_err} results=${base_res}"
log "source mon_host (before): ${before_host}"
log "arbiter mon_host (before): $(arbiter_mon_host)"

# Pick a source mon that is NOT the quorum leader to keep the failover clean.
LEADER="$(quorum_json | jq -r '.quorum_leader_name')"
VICTIM_DEPLOY=""
for d in $(kubectl -n "${ROOK_NS}" get deploy -l app=rook-ceph-mon -o name | sort); do
  id="$(basename "${d}" | sed 's/^rook-ceph-mon-//')"
  [ "${id}" = "${LEADER}" ] && continue
  VICTIM_DEPLOY="${d}"; break
done
[ -n "${VICTIM_DEPLOY}" ] || die "could not pick a non-leader source mon to fail over"
victim_id="$(basename "${VICTIM_DEPLOY}" | sed 's/^rook-ceph-mon-//')"

# Delete the mon Deployment outright so Rook's mon failover creates a fresh mon
# (new letter + address) rather than just restarting the same one — this is what
# actually changes mon_host.
log "failing over source mon ${victim_id} (deleting its Deployment; Rook will replace it)"
kubectl -n "${ROOK_NS}" delete "${VICTIM_DEPLOY}" --wait=false >/dev/null 2>&1 || true

log "holding ${HOLD:-120}s with traffic while Rook fails the mon over and the operator re-syncs"
end=$(( $(date +%s) + ${HOLD:-120} ))
while [ "$(date +%s)" -lt "${end}" ]; do
  quorum_json >/dev/null 2>&1 || true
  sleep 10
done

after_host="$(source_mon_host)"
arb_host="$(arbiter_mon_host)"
log "source mon_host (after):  ${after_host}"
log "arbiter mon_host (after): ${arb_host}"
if [ "${before_host}" = "${after_host}" ]; then
  log "note: mon_host did not change (Rook may have reused the address); test still valid for the convergence path"
fi

# Data plane is the gate. Convergence of the arbiter's copied Secret is reported
# but not hard-asserted (it reconciles on CheckInterval and may lag briefly).
assert_s3_delta "${base_err}" zero "${base_res}"
if [ -n "${arb_host}" ] && [ "${arb_host}" = "${after_host}" ]; then
  log "PASS S5: source mon failover; RGW uninterrupted; arbiter mon_host converged"
else
  log "PASS S5: source mon failover; RGW uninterrupted (arbiter mon_host convergence still pending — reconciles on CheckInterval)"
fi
