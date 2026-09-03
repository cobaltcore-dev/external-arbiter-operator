#!/usr/bin/env bash
# run.sh — full quorum-survival e2e. Brings up k3d + Rook + operator + arbiter,
# proves the arbiter votes, kills a source mon, proves quorum survives.
#
#   bash test/e2e/quorum/run.sh                 # full run, teardown on exit
#   KEEP_CLUSTER=1 bash .../run.sh              # leave cluster up for debugging
#   NEGATIVE_CONTROL=1 bash .../run.sh          # kill 2 mons, expect quorum LOST
#
# Needs docker + k3d + kubectl + jq + helm, ~16GB RAM, ~12min cold.
set -euo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=test/e2e/quorum/lib.sh
source "${HERE}/lib.sh"

for t in docker k3d kubectl jq helm; do need "${t}"; done

# Always tear down (unless KEEP_CLUSTER=1) even if a step fails.
cleanup() { bash "${HERE}/99-teardown.sh" || true; }
trap cleanup EXIT

bash "${HERE}/00-k3d-up.sh"
bash "${HERE}/10-rook-install.sh"
bash "${HERE}/20-cephcluster.sh"
bash "${HERE}/30-operator-deploy.sh"
bash "${HERE}/40-remote-kubeconfig.sh"
bash "${HERE}/50-apply-arbiter.sh"
bash "${HERE}/60-assert-baseline.sh"
bash "${HERE}/70-kill-mon.sh"
bash "${HERE}/80-assert-survival.sh"

log "ALL PASS — quorum-survival property proven end to end"