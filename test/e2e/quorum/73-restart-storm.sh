# Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
# SPDX-License-Identifier: Apache-2.0
#!/usr/bin/env bash
# 73 — Scenario S3: arbiter mon restart storm must not disrupt RGW.
#
# S3 in docs/rgw-failure-scenarios.md: operator drift detection
# (checkArbiterDeploymentUpToDate / restartArbiterDeployment,
# remotearbiter_controller.go) can restart the arbiter mon repeatedly. Each
# restart drops the arbiter from quorum until it re-joins; repeated flaps churn
# the mon map. We force drift by bumping a watched source resource and verify
# RGW stays up through the flapping.
#
# The operator watches Secrets/ConfigMaps/Deployments OWNED by the CephCluster
# (findArbiterForCephClusterOwnedObject). Annotating rook-config-override bumps
# its resourceVersion (ResourceVersionChangedPredicate) and drives a reconcile,
# exercising the drift/update path.
#
# Expectation: zero S3 error delta despite arbiter restarts. We also report the
# arbiter Deployment's observed restart/generation churn for visibility.
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

arbiter_rs_count() {
  # ReplicaSet count is a proxy for how many times the arbiter Deployment's pod
  # template changed (each template change spawns a new RS).
  kubectl -n "${TARGET_NS}" get rs \
    -l ceph.cobaltcore.sap.com/lookup=external-arbiter \
    -o name 2>/dev/null | wc -l | tr -d ' '
}
arbiter_restarts() {
  kubectl -n "${TARGET_NS}" get pod \
    -l ceph.cobaltcore.sap.com/lookup=external-arbiter \
    -o jsonpath='{range .items[*]}{.status.containerStatuses[*].restartCount}{"\n"}{end}' 2>/dev/null \
    | awk '{s+=$1} END{print s+0}'
}

restore_all() { stop_s3_traffic; }
trap restore_all EXIT INT TERM

start_s3_traffic
base_err="$(s3_error_count)"; base_res="$(s3_result_count)"
rs_before="$(arbiter_rs_count)"; restarts_before="$(arbiter_restarts)"
log "baseline: S3 errors=${base_err} results=${base_res} arbiter RS=${rs_before} restarts=${restarts_before}"

cycles="${STORM_CYCLES:-8}"
log "driving ${cycles} drift cycles by annotating rook-config-override"
for i in $(seq 1 "${cycles}"); do
  kubectl -n "${ROOK_NS}" annotate configmap rook-config-override \
    "arbiter-chaos/storm=${i}-$(date +%s)" --overwrite >/dev/null 2>&1 || true
  # keep traffic flowing and let the operator reconcile between bumps
  hold_with_traffic "${STORM_STEP:-12}" 4
done

log "holding ${HOLD:-30}s for the last restart to settle"
hold_with_traffic "${HOLD:-30}"

rs_after="$(arbiter_rs_count)"; restarts_after="$(arbiter_restarts)"
log "after storm: arbiter RS=${rs_after} (was ${rs_before}) restarts=${restarts_after} (was ${restarts_before})"

# The arbiter must still be a voting member after the storm settles.
Q="$(quorum_json)"
A=$(echo "${Q}" | jq '[.quorum_names[] | select(startswith("ext-"))] | length')
[ "${A}" -ge 1 ] || die "arbiter not in quorum after restart storm: $(echo "${Q}" | jq -c '.quorum_names')"

assert_s3_delta "${base_err}" zero "${base_res}"
log "PASS S3: arbiter restart storm did not interrupt RGW (arbiter still voting)"
