# shellcheck shell=bash
# shellcheck disable=SC2034  # vars are consumed by the scripts that source this file
# Shared config + helpers for the quorum-survival e2e harness.
# Sourced by every numbered step and by run.sh. No side effects on source.

CLUSTER="${CLUSTER:-arbiter-e2e}"
ROOK_VERSION="${ROOK_VERSION:-1.18.6}"
CERT_MANAGER_VERSION="${CERT_MANAGER_VERSION:-v1.19.2}"

ROOK_NS="rook-ceph"
OP_NS="arbiter-operator"       # operator + CRs + accesskeyRef Secret (README convention)
TARGET_NS="external-arbiter"   # where the arbiter mon Deployment lands

IMAGE="localhost:5000/cobaltcore-dev/external-arbiter-operator:latest"

# Where 70-kill-mon.sh records the killed ceph mon id(s) for 80 to assert absent.
VICTIM_FILE="${VICTIM_FILE:-${TMPDIR:-/tmp}/arbiter-e2e-victims}"

# Repo root = two levels up from this file (test/e2e/quorum/).
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${HERE}/../../.." && pwd)"
MANIFESTS="${HERE}/manifests"
CHART="${REPO_ROOT}/contrib/charts/external-arbiter-operator"

log()  { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
die()  { printf '\n\033[1;31mFAIL: %s\033[0m\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "required tool not found: $1"; }

rook_url() { printf 'https://raw.githubusercontent.com/rook/rook/v%s/deploy/examples/%s' "${ROOK_VERSION}" "$1"; }

# The rook-ceph-tools pod name (Ceph CLI runs inside it).
toolbox_pod() { kubectl -n "${ROOK_NS}" get pod -l app=rook-ceph-tools -o name | head -1; }

# Run a ceph command in the toolbox with a bounded connection timeout, so a
# lost-quorum cluster fails fast instead of hanging (ceph blocks waiting for a
# mon otherwise). Also wrap in a host-side timeout when one is available
# (macOS base has none; coreutils provides gtimeout).
TIMEOUT_BIN="$(command -v timeout || command -v gtimeout || true)"
ceph_exec() {
  local tb; tb="$(toolbox_pod)"
  [ -n "${tb}" ] || die "no rook-ceph-tools pod"
  if [ -n "${TIMEOUT_BIN}" ]; then
    "${TIMEOUT_BIN}" 60 kubectl -n "${ROOK_NS}" exec "${tb}" -- "$@" --connect-timeout 15
  else
    kubectl -n "${ROOK_NS}" exec "${tb}" -- "$@" --connect-timeout 15
  fi
}

# ceph quorum_status as JSON, run in the toolbox.
quorum_json() { ceph_exec ceph quorum_status --format json; }