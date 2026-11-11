# Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
# SPDX-License-Identifier: Apache-2.0
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

# ---- RGW data-plane (chaos extension) ----------------------------------------
# Used by 25-objectstore.sh and the 71..77 chaos steps. The traffic oracle runs
# an in-cluster S3 probe against this store's RGW ClusterIP (the RGW service is
# ClusterIP-only, so traffic must originate inside the cluster, not the host).
RGW_STORE="${RGW_STORE:-my-store}"
RGW_USER="${RGW_USER:-test-user}"
RGW_SECRET="rook-ceph-object-user-${RGW_STORE}-${RGW_USER}"
RGW_SVC="rook-ceph-rgw-${RGW_STORE}"
# In-cluster probe pod + image. awscli image keeps the probe dependency-free.
S3_PROBE_POD="${S3_PROBE_POD:-arbiter-s3-probe}"
S3_PROBE_IMAGE="${S3_PROBE_IMAGE:-amazon/aws-cli:2.15.30}"
S3_PROBE_BUCKET="${S3_PROBE_BUCKET:-arbiter-chaos}"
# Host-side marker file recording where the probe's result log lives in-pod.
S3_PROBE_STARTED="${S3_PROBE_STARTED:-${TMPDIR:-/tmp}/arbiter-e2e-s3-started}"

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

# Client-facing mon-config fingerprint. Rook publishes mon endpoints to clients
# (RGW included) via the rook-ceph-mon-endpoints ConfigMap and the
# rook-ceph-config Secret (NOTE: rook-ceph-config is a *Secret*, not a
# ConfigMap). A change in either resourceVersion is a mon-config rewrite that a
# client could re-read. Returns "<cm-rv>/<secret-rv>" (missing objects → "x").
mon_config_fingerprint() {
  local cm sec
  cm="$(kubectl -n "${ROOK_NS}" get configmap rook-ceph-mon-endpoints \
    -o jsonpath='{.metadata.resourceVersion}' 2>/dev/null || true)"
  sec="$(kubectl -n "${ROOK_NS}" get secret rook-ceph-config \
    -o jsonpath='{.metadata.resourceVersion}' 2>/dev/null || true)"
  printf '%s/%s' "${cm:-x}" "${sec:-x}"
}

# ---- RGW traffic oracle ------------------------------------------------------
# The oracle is an in-cluster pod that, in a tight loop, does PUT then GET then
# LIST of a small object against the RGW ClusterIP and appends a single result
# line per iteration to /tmp/s3-probe.log in the pod:
#   <epoch> OK            — all three ops (write/read/list) succeeded
#   <epoch> ERR write     — a write/read/list op failed (op named)
# Error counts are derived by grep-counting ERR lines, so any window's error
# delta is just (count_after - count_before). This is the data-plane analogue of
# quorum_json for the control plane: it answers "did S3 break?" directly.

# Credentials + endpoint are resolved once, inside the pod, from the Rook Secret
# and the RGW service DNS name (reachable in-cluster). The probe writes unique
# keys per iteration so retries never collide.
s3_probe_running() {
  kubectl -n "${ROOK_NS}" get pod "${S3_PROBE_POD}" \
    -o jsonpath='{.status.phase}' 2>/dev/null | grep -qx Running
}

# One-line status of the probe pod: "<phase> (<container-reason>)" — used to make
# the startup wait verbose so a slow image pull / scheduling is visible.
s3_probe_status() {
  local phase reason
  phase="$(kubectl -n "${ROOK_NS}" get pod "${S3_PROBE_POD}" \
    -o jsonpath='{.status.phase}' 2>/dev/null || true)"
  reason="$(kubectl -n "${ROOK_NS}" get pod "${S3_PROBE_POD}" \
    -o jsonpath='{.status.containerStatuses[0].state.waiting.reason}' 2>/dev/null || true)"
  [ -z "${reason}" ] && reason="$(kubectl -n "${ROOK_NS}" get pod "${S3_PROBE_POD}" \
    -o jsonpath='{.status.containerStatuses[0].state.running.startedAt}' 2>/dev/null \
    | sed 's/.*/running/' || true)"
  printf '%s (%s)' "${phase:-<none>}" "${reason:-?}"
}

# Start the continuous S3 probe pod (idempotent). Blocks until it is Running and
# has logged at least one result line, so callers can immediately baseline.
start_s3_traffic() {
  if s3_probe_running; then
    log "S3 traffic oracle already running"
    return 0
  fi
  local ak sk
  ak="$(kubectl -n "${ROOK_NS}" get secret "${RGW_SECRET}" -o jsonpath='{.data.AccessKey}' | base64 -d)"
  sk="$(kubectl -n "${ROOK_NS}" get secret "${RGW_SECRET}" -o jsonpath='{.data.SecretKey}' | base64 -d)"
  [ -n "${ak}" ] && [ -n "${sk}" ] || die "could not read S3 credentials from ${RGW_SECRET} (run 25-objectstore.sh first)"

  log "starting in-cluster S3 traffic oracle pod ${S3_PROBE_POD}"
  kubectl -n "${ROOK_NS}" delete pod "${S3_PROBE_POD}" --ignore-not-found --wait=false >/dev/null 2>&1 || true

  # The probe script runs inside the pod. It is intentionally dependency-light:
  # only the aws CLI (from the image) plus coreutils. --endpoint-url targets the
  # in-cluster RGW service DNS; --no-verify-ssl + http keep it simple for lab.
  #
  # Region MUST be us-east-1: RGW's default zonegroup only accepts the S3
  # "no location constraint" region. A region like eu-central-1 makes the CLI
  # send a CreateBucketConfiguration that RGW rejects with
  # InvalidLocationConstraint, so the bucket is never created and the probe logs
  # nothing — making every "0 errors" result vacuous.
  kubectl -n "${ROOK_NS}" run "${S3_PROBE_POD}" \
    --image="${S3_PROBE_IMAGE}" --restart=Never \
    --env="AWS_ACCESS_KEY_ID=${ak}" \
    --env="AWS_SECRET_ACCESS_KEY=${sk}" \
    --env="AWS_DEFAULT_REGION=us-east-1" \
    --env="AWS_REGION=us-east-1" \
    --env="EP=http://${RGW_SVC}.${ROOK_NS}.svc:80" \
    --env="BUCKET=${S3_PROBE_BUCKET}" \
    --command -- /bin/sh -c '
      log=/tmp/s3-probe.log; : > "$log"
      aws="aws --endpoint-url $EP --no-verify-ssl --region us-east-1"
      # ensure bucket; record the last creation error so a stuck loop is
      # diagnosable via the pod log instead of spinning silently forever.
      until $aws s3api head-bucket --bucket "$BUCKET" >/dev/null 2>&1; do
        err="$($aws s3 mb "s3://$BUCKET" 2>&1)" && break
        echo "$(date +%s) BUCKET-WAIT $err" >> "$log"
        sleep 2
      done
      echo "$(date +%s) BUCKET-READY" >> "$log"
      i=0
      while true; do
        i=$((i+1)); key="probe-$$-$i"; f=/tmp/obj
        head -c 1024 /dev/urandom > "$f" 2>/dev/null
        ts=$(date +%s)
        if ! $aws s3 cp "$f" "s3://$BUCKET/$key" >/dev/null 2>&1; then
          echo "$ts ERR write" >> "$log"; sleep 1; continue; fi
        if ! $aws s3 cp "s3://$BUCKET/$key" /tmp/got >/dev/null 2>&1; then
          echo "$ts ERR read" >> "$log"; sleep 1; continue; fi
        if ! $aws s3 ls "s3://$BUCKET/" >/dev/null 2>&1; then
          echo "$ts ERR list" >> "$log"; sleep 1; continue; fi
        echo "$ts OK" >> "$log"
        $aws s3 rm "s3://$BUCKET/$key" >/dev/null 2>&1 || true
        sleep 1
      done' >/dev/null

  log "waiting for S3 oracle pod ${S3_PROBE_POD} to become Running"
  log "  (first run pulls ${S3_PROBE_IMAGE} — may take a minute)"
  local _ i=0 last=""
  for _ in $(seq 1 90); do
    s3_probe_running && break
    i=$((i + 1))
    local st; st="$(s3_probe_status)"
    # only print when the status changes, so slow pulls show progress not spam
    if [ "${st}" != "${last}" ]; then
      printf '    [%3ds] pod: %s\n' "$((i * 2))" "${st}"
      last="${st}"
      # surface a stuck image pull loudly
      case "${st}" in
        *ImagePullBackOff*|*ErrImagePull*)
          printf '    \033[33mimage pull is failing — check registry access from the cluster\033[0m\n' ;;
      esac
    fi
    sleep 2
  done
  if ! s3_probe_running; then
    log "S3 oracle pod never reached Running — last status: $(s3_probe_status)"
    kubectl -n "${ROOK_NS}" describe pod "${S3_PROBE_POD}" 2>/dev/null \
      | sed -n '/Events:/,$p' | head -20 || true
    die "S3 oracle pod ${S3_PROBE_POD} did not start"
  fi
  log "S3 oracle pod Running — waiting for first probe result (ensuring bucket ${S3_PROBE_BUCKET})"
  local n=0 w=0
  for _ in $(seq 1 90); do
    n="$(s3_result_count)"
    [ "${n}" -ge 1 ] 2>/dev/null && break
    w=$((w + 1))
    # every ~10s show the latest setup state so a stuck bucket-create (e.g. an
    # InvalidLocationConstraint) is visible instead of an opaque silent wait.
    if [ $(( w % 5 )) -eq 0 ]; then
      local tail1; tail1="$(s3_recent 1 | tr -d '\r')"
      printf '    [%3ds] results=%s | last probe log: %s\n' "$(( w * 2 ))" "${n}" "${tail1:-<none>}"
    fi
    sleep 2
  done
  [ "${n:-0}" -ge 1 ] 2>/dev/null \
    || log "warning: oracle Running but no result logged yet — continuing (baseline will be 0)"
  : > "${S3_PROBE_STARTED}"
  log "S3 traffic oracle live (logged ${n} result line(s), $(s3_error_count) error(s) so far)"
}

# Read a grep -c from the probe pod's result log, with retries.
# kubectl exec goes through the API server; on a loaded cluster (restart
# storms, leader-election churn) a single exec can time out. An exec failure
# prints nothing, which callers must not misread as "count is zero".
# Prints the count on success; prints nothing if the oracle is unreachable.
s3_count_read() {
  local pattern="$1" out i
  for i in 1 2 3; do
    out="$(kubectl -n "${ROOK_NS}" exec "${S3_PROBE_POD}" -- \
      sh -c "grep -cE '${pattern}' /tmp/s3-probe.log" 2>/dev/null | tr -dc '0-9')"
    if [ -n "${out}" ]; then
      echo "${out}"
      return 0
    fi
    sleep 3
  done
  return 1
}

# Result lines the probe has logged (OK + ERR only — excludes BUCKET-* setup
# lines). This is the "is traffic actually flowing?" counter.
s3_result_count() {
  s3_count_read " (OK|ERR )" || echo 0
}

# Back-compat alias used by the startup wait.
s3_total_count() { s3_result_count; }

# Cumulative error result lines (" ERR <op>"). Anchored on the leading space so
# RGW error TEXT inside a BUCKET-WAIT diagnostic line can never be miscounted.
s3_error_count() {
  s3_count_read " ERR " || echo 0
}

# Dump the most recent probe log lines (debugging / failure messages), including
# BUCKET-* setup lines so a stuck bucket-create is visible.
s3_recent() {
  local n="${1:-20}"
  kubectl -n "${ROOK_NS}" exec "${S3_PROBE_POD}" -- \
    sh -c "tail -n ${n} /tmp/s3-probe.log 2>/dev/null" 2>/dev/null || true
}

# Stop + delete the probe pod. Safe to call in an EXIT trap.
stop_s3_traffic() {
  kubectl -n "${ROOK_NS}" delete pod "${S3_PROBE_POD}" --ignore-not-found --wait=false >/dev/null 2>&1 || true
  rm -f "${S3_PROBE_STARTED}" 2>/dev/null || true
}

# Hold for N seconds while the oracle keeps running, sampling quorum so a stall
# surfaces in logs. Args: $1 = seconds (default 60), $2 = sample step (default 5).
hold_with_traffic() {
  local total="${1:-60}" step="${2:-5}" t=0
  while [ "${t}" -lt "${total}" ]; do
    quorum_json >/dev/null 2>&1 || true
    sleep "${step}"; t=$((t + step))
  done
}

# Assert the S3 error delta since a baseline. Args:
#   $1 = baseline ERROR count (from s3_error_count before the fault)
#   $2 = expectation: "zero" (no new errors) | "nonzero" (expect >=1 new error)
#   $3 = (optional) baseline RESULT count (from s3_result_count before the fault)
# When $3 is given, the assert also verifies that traffic ACTUALLY FLOWED during
# the window (new result lines were logged). This prevents a vacuous "0 errors"
# pass when the probe never ran a single op (e.g. bucket creation stuck).
assert_s3_delta() {
  local base="$1" expect="$2" base_results="${3:-}" now delta
  now="$(s3_error_count)"; delta=$(( now - base ))

  if [ -n "${base_results}" ]; then
    local now_results res_delta
    now_results="$(s3_result_count)"; res_delta=$(( now_results - base_results ))
    if [ "${res_delta}" -le 0 ]; then
      # Distinguish infra failure from genuinely stalled traffic: an exec
      # failure yields no count at all (oracle unreachable), while a count
      # below baseline means the probe pod restarted and truncated its log.
      if ! s3_count_read " (OK|ERR )" >/dev/null; then
        die "S3 oracle unreachable (kubectl exec failed after retries) — cannot assess traffic, result is inconclusive"
      fi
      if [ "${now_results}" -lt "${base_results}" ]; then
        log "WARN: result count went backwards (${base_results} -> ${now_results}) — probe pod likely restarted; baseline unreliable"
      fi
      log "recent probe lines:"; s3_recent 30
      die "S3 oracle logged no new results during the window (${res_delta}) — traffic never flowed, result is vacuous"
    fi
    log "oracle processed ${res_delta} S3 op-cycles during the window"
  fi

  case "${expect}" in
    zero)
      if [ "${delta}" -eq 0 ]; then
        log "PASS: RGW uninterrupted (0 new S3 errors)"
      else
        log "recent probe lines:"; s3_recent 30
        die "RGW interrupted: ${delta} new S3 errors during fault"
      fi ;;
    nonzero)
      if [ "${delta}" -ge 1 ]; then
        log "PASS (expected impact): ${delta} new S3 errors — fault is load-bearing"
      else
        die "expected S3 errors during fault but saw 0 (test may be vacuous)"
      fi ;;
    *) die "assert_s3_delta: unknown expectation '${expect}'" ;;
  esac
}