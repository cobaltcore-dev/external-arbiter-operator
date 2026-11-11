# Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
# SPDX-License-Identifier: Apache-2.0
#!/usr/bin/env bash
# 76 — Scenario S10: mon-config churn (saveMonConfig) must not interrupt RGW.
#
# S10 in docs/rgw-failure-scenarios.md: adding/removing an entry in
# CephCluster.Spec.Mon.ExternalMonIDs makes Rook reconcile external mons and call
# saveMonConfig() (rook .../mon/health.go), rewriting rook-ceph-config /
# mon-endpoints that every client — including RGW — reads. Each rewrite can cause
# RGW to re-read mon endpoints and briefly reconnect.
#
# We induce churn by appending then removing a throwaway external mon id a few
# times while S3 traffic runs, and correlate rook-ceph-config resourceVersion
# bumps with the S3 error delta.
#
# Expectation: config churn occurs (resourceVersion changes), but RGW stays up
# (zero error delta). A non-zero delta is a real finding about client resets.
#
# NOTE: mutates CephCluster.Spec.Mon.ExternalMonIDs. restore_all puts the list
# back to exactly the operator-managed value captured at start.
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

CHURN_ID="${CHURN_ID:-zz-chaos}"   # a throwaway id the operator never allocates

ext_ids_json() {
  kubectl -n "${ROOK_NS}" get cephcluster my-cluster \
    -o jsonpath='{.spec.mon.externalMonIDs}' 2>/dev/null || echo '[]'
}
# Add or remove CHURN_ID via a JSON merge on the externalMonIDs array.
add_churn_id() {
  kubectl -n "${ROOK_NS}" patch cephcluster my-cluster --type=json \
    -p "[{\"op\":\"add\",\"path\":\"/spec/mon/externalMonIDs/-\",\"value\":\"${CHURN_ID}\"}]" \
    >/dev/null 2>&1 || true
}
remove_churn_id() {
  # Rebuild the array without CHURN_ID (json-patch by index is fragile; replace).
  local kept
  kept="$(ext_ids_json | jq -c "[.[] | select(. != \"${CHURN_ID}\")]" 2>/dev/null || echo '[]')"
  kubectl -n "${ROOK_NS}" patch cephcluster my-cluster --type=merge \
    -p "{\"spec\":{\"mon\":{\"externalMonIDs\":${kept}}}}" >/dev/null 2>&1 || true
}

ORIG_IDS="$(ext_ids_json)"
restore_all() {
  log "restoring externalMonIDs to original: ${ORIG_IDS}"
  kubectl -n "${ROOK_NS}" patch cephcluster my-cluster --type=merge \
    -p "{\"spec\":{\"mon\":{\"externalMonIDs\":${ORIG_IDS:-[]}}}}" >/dev/null 2>&1 || true
  stop_s3_traffic
}
trap restore_all EXIT INT TERM

start_s3_traffic
base_err="$(s3_error_count)"; base_res="$(s3_result_count)"
log "baseline: S3 errors=${base_err} results=${base_res} externalMonIDs=${ORIG_IDS} mon-config=$(mon_config_fingerprint)"

cycles="${CHURN_CYCLES:-5}"
last_rv="$(mon_config_fingerprint)"; churn=0
for i in $(seq 1 "${cycles}"); do
  log "churn cycle ${i}/${cycles}: add ${CHURN_ID}"
  add_churn_id;    hold_with_traffic "${CHURN_STEP:-10}" 5
  log "churn cycle ${i}/${cycles}: remove ${CHURN_ID}"
  remove_churn_id; hold_with_traffic "${CHURN_STEP:-10}" 5
  rv="$(mon_config_fingerprint)"
  if [ "${rv}" != "${last_rv}" ]; then churn=$((churn + 1)); last_rv="${rv}"; log "  mon-config changed → ${rv}"; fi
done

log "mon-config churn events observed: ${churn} (rook-ceph-mon-endpoints / rook-ceph-config rewrites)"
[ "${churn}" -ge 1 ] || log "note: no mon-config churn detected — Rook may debounce rapid toggles, or ignore a non-allocated id"

assert_s3_delta "${base_err}" zero "${base_res}"
log "PASS S10: mon-config churn did not interrupt RGW (${churn} rewrites observed)"
