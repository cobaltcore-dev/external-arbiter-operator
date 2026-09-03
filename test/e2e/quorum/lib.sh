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

# ceph quorum_status as JSON, run in the toolbox.
quorum_json() {
  local tb; tb="$(toolbox_pod)"
  [ -n "${tb}" ] || die "no rook-ceph-tools pod"
  kubectl -n "${ROOK_NS}" exec "${tb}" -- ceph quorum_status --format json
}