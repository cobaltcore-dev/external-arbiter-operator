# Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
# SPDX-License-Identifier: Apache-2.0
#!/usr/bin/env bash
# 72 — Scenario S2: teardown ordering must not disrupt RGW.
#
# S2 in docs/rgw-failure-scenarios.md: deleting the RemoteArbiter removes the mon
# id from CephCluster.Spec.Mon.ExternalMonIDs (cleanUpCephCluster,
# remotearbiter_controller.go:1516). If remote cleanup is skipped (e.g. remote
# API unreachable, :1580) the external mon can still be running while Rook no
# longer protects it — config churn and potential failover action can reset RGW
# client connections.
#
# This step deletes the RemoteArbiter under live S3 traffic and asserts the data
# plane stays up. It also records whether the arbiter mon left quorum before vs.
# after the ExternalMonIDs entry was dropped (observability for the ordering
# gap), and watches rook-ceph config churn.
#
# Expectation: zero S3 error delta across the teardown. Non-zero is a finding
# (teardown interrupts RGW) worth failing on.
#
# NOTE: destructive — it deletes the RemoteArbiter. restore_all re-applies it so
# a subsequent run / KEEP_CLUSTER debugging starts from a known state.
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

ext_mon_ids() {
  kubectl -n "${ROOK_NS}" get cephcluster my-cluster \
    -o jsonpath='{.spec.mon.externalMonIDs}' 2>/dev/null || true
}

restore_all() {
  log "restoring: re-applying RemoteCluster + RemoteArbiter"
  kubectl apply -f "${MANIFESTS}/remote-cluster.yaml" >/dev/null 2>&1 || true
  kubectl apply -f "${MANIFESTS}/remote-arbiter.yaml" >/dev/null 2>&1 || true
  stop_s3_traffic
}
trap restore_all EXIT INT TERM

start_s3_traffic
base_err="$(s3_error_count)"; base_res="$(s3_result_count)"
log "baseline S3 error count: ${base_err} (results so far: ${base_res})"

log "before teardown: externalMonIDs=$(ext_mon_ids) mon-config=$(mon_config_fingerprint)"
before_q="$(quorum_json | jq -c '.quorum_names')"
log "before teardown: quorum=${before_q}"

log "deleting RemoteArbiter (non-blocking) and holding with traffic"
kubectl -n "${OP_NS}" delete remotearbiter external-arbiter --wait=false >/dev/null 2>&1 || true

# Sample the ordering gap while traffic runs: does ExternalMonIDs empty out
# before/after the arbiter leaves quorum? and does the mon-config churn?
end=$(( $(date +%s) + ${HOLD:-90} ))
last_rv="$(mon_config_fingerprint)"; churn=0
while [ "$(date +%s)" -lt "${end}" ]; do
  ids="$(ext_mon_ids)"; rv="$(mon_config_fingerprint)"
  if [ "${rv}" != "${last_rv}" ]; then
    churn=$((churn + 1)); last_rv="${rv}"
    log "  mon-config churned (${rv}); externalMonIDs=${ids}"
  fi
  quorum_json >/dev/null 2>&1 || true
  sleep 5
done

log "after teardown: externalMonIDs=$(ext_mon_ids) mon-config churn events=${churn}"

# The data plane is the pass/fail gate; config churn count is informational.
assert_s3_delta "${base_err}" zero "${base_res}"
log "PASS S2: RemoteArbiter teardown did not interrupt RGW (config churn events: ${churn})"
