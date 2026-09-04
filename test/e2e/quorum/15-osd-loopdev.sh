#!/usr/bin/env bash
# 15 — attach a raw loop-backed block device to every node (Rook useAllDevices
# needs a raw device; k3d nodes have no spare disk). Must run BEFORE the
# CephCluster so the device exists when Rook scans for OSDs.
set -euo pipefail
# shellcheck source=test/e2e/quorum/lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

log "attaching loop devices via DaemonSet"
kubectl apply -f "${MANIFESTS}/osd-loopdev-daemonset.yaml"
kubectl -n "${ROOK_NS}" rollout status ds/osd-loopdev --timeout=120s
log "loop devices attached on all nodes"