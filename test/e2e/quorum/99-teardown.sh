# Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
# SPDX-License-Identifier: Apache-2.0
#!/usr/bin/env bash
# 99 — teardown. Deletes the k3d cluster (KEEP_CLUSTER=1 to keep it for debug).
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

if [ "${KEEP_CLUSTER:-0}" = "1" ]; then
  log "KEEP_CLUSTER=1 — leaving ${CLUSTER} running"
  exit 0
fi
# k3d is only used by the from-scratch (FULL=1) flow. When running against an
# existing cluster (e.g. the Lima VM, no Docker/k3d installed), there is nothing
# for us to delete — skip gracefully instead of erroring.
if ! command -v k3d >/dev/null 2>&1; then
  log "k3d not installed — nothing to tear down (external/existing cluster)"
  exit 0
fi
if k3d cluster list -o json | jq -e --arg n "${CLUSTER}" '.[] | select(.name==$n)' >/dev/null 2>&1; then
  log "deleting k3d cluster ${CLUSTER}"
  k3d cluster delete "${CLUSTER}"
fi