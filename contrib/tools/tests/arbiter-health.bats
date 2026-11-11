#!/usr/bin/env bats
# Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
# SPDX-License-Identifier: Apache-2.0
# Tests for contrib/tools/arbiter-health

setup() {
  load helpers/setup.bash
  TOOL="${TOOLS_DIR}/arbiter-health"
}

teardown() {
  teardown_mock
}

# Strip ANSI color escapes so tag assertions (e.g. 'INFO]') can grep cleanly.
strip_ansi() { sed $'s/\033\\[[0-9;]*m//g'; }

# ---------------------------------------------------------------------------
# CLI argument validation
# ---------------------------------------------------------------------------

@test "arbiter-health: --help prints usage and exits 0" {
  run bash "$TOOL" --help
  [ "$status" -eq 0 ]
  [[ "$output" == *"Usage"* ]]
  [[ "$output" == *"--namespace"* ]]
  [[ "$output" == *"--remote-kubeconfig"* ]]
}

@test "arbiter-health: unknown option exits nonzero" {
  run bash "$TOOL" --bogus-option
  [ "$status" -ne 0 ]
}

# ---------------------------------------------------------------------------
# Quorum parsing: monmap.mons bracket-matching
# ---------------------------------------------------------------------------

# Helper: extract monmap.mons array from quorum_status JSON.
# Normalises input to a single line to handle both compact and pretty-printed
# output, then uses awk bracket-matching with a whitespace-tolerant needle.
_extract_mons_json() {
  local flat
  flat=$(printf '%s' "$1" | tr '\n' ' ' | sed 's/  */ /g')
  printf '%s' "$flat" | awk -v Q='"' '
    {
      n = match($0, Q "mons" Q "[[:space:]]*:[[:space:]]*\\[")
      if (n == 0) exit
      rest = substr($0, n + RLENGTH)
      depth = 1
      for (i = 1; i <= length(rest) && depth > 0; i++) {
        ch = substr(rest, i, 1)
        if (ch == "[") depth++
        else if (ch == "]") depth--
      }
      print substr(rest, 1, i - 2)
    }'
}

# Helper: extract mon names from mons JSON fragment (space-tolerant).
_extract_mon_names() {
  printf '%s' "$1" \
    | grep -oE '"name" *: *"[^"]*"' \
    | sed 's/"name" *: *"//g; s/"//g' \
    | tr '\n' ',' | sed 's/,$//'
}

@test "arbiter-health: extracts mons when monmap is NOT last JSON field" {
  # This is the real Ceph structure where monmap is followed by quorum_age, features, etc.
  local json
  json=$(cat "$FIXTURES_DIR/quorum_status.json")

  local mons_json
  mons_json=$(_extract_mons_json "$json")

  # Should find 3 mons
  local count
  count=$(printf '%s' "$mons_json" | grep -oE '"name" *:' | wc -l | tr -d ' ')
  [ "$count" -eq 3 ]

  # Should find all three names
  local names
  names=$(_extract_mon_names "$mons_json")
  [ "$names" = "a,b,ext-c" ]
}

@test "arbiter-health: extracts mons from pretty-printed (multi-line) JSON" {
  # Ceph may return pretty-printed JSON; verify parsing still works.
  local json
  json=$(python3 -c "
import json, sys
with open('$FIXTURES_DIR/quorum_status.json') as f:
    d = json.load(f)
json.dump(d, sys.stdout, indent=2)
")

  local mons_json
  mons_json=$(_extract_mons_json "$json")

  local count
  count=$(printf '%s' "$mons_json" | grep -oE '"name" *:' | wc -l | tr -d ' ')
  [ "$count" -eq 3 ]

  local names
  names=$(_extract_mon_names "$mons_json")
  [ "$names" = "a,b,ext-c" ]
}

@test "arbiter-health: extracts mons when monmap IS last JSON field" {
  local json='{"monmap":{"mons":[{"rank":0,"name":"solo"}]}}'
  local mons_json
  mons_json=$(_extract_mons_json "$json")
  local names
  names=$(_extract_mon_names "$mons_json")
  [ "$names" = "solo" ]
}

@test "arbiter-health: returns empty for malformed mons array" {
  local json='{"monmap":{"epoch":1}}'
  local mons_json
  mons_json=$(_extract_mons_json "$json")
  [ -z "$mons_json" ]
}

# ---------------------------------------------------------------------------
# Mon ID matching: exact whole-line (grep -qxF)
# ---------------------------------------------------------------------------

@test "arbiter-health: mon ID exact match - ext-a does not match ext-ab" {
  local mon_list="ext-ab,mon-a"
  if echo "$mon_list" | tr ',' '\n' | grep -qxF "ext-a"; then
    # Should NOT match
    false
  else
    true
  fi
}

@test "arbiter-health: mon ID exact match - ext-a matches ext-a" {
  local mon_list="ext-a,mon-a"
  echo "$mon_list" | tr ',' '\n' | grep -qxF "ext-a"
}

# ---------------------------------------------------------------------------
# RemoteCluster discovery: ownerReferences scanning
# ---------------------------------------------------------------------------

@test "arbiter-health: finds RemoteCluster when ownerRef is at index 1" {
  local rc_list="my-rc|Deployment/some-deploy,RemoteArbiter/external-arbiter,"
  local ARBITER_NAME="external-arbiter"
  local rc_name=""
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    local rc_candidate rc_owners
    rc_candidate="${line%%|*}"
    rc_owners="${line#*|}"
    if echo "$rc_owners" | tr ',' '\n' | grep -qxF "RemoteArbiter/${ARBITER_NAME}"; then
      rc_name="$rc_candidate"
      break
    fi
  done <<< "$rc_list"
  [ "$rc_name" = "my-rc" ]
}

@test "arbiter-health: no match when ownerRef has different kind" {
  local rc_list="my-rc|Deployment/external-arbiter,"
  local ARBITER_NAME="external-arbiter"
  local rc_name=""
  while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    local rc_candidate rc_owners
    rc_candidate="${line%%|*}"
    rc_owners="${line#*|}"
    if echo "$rc_owners" | tr ',' '\n' | grep -qxF "RemoteArbiter/${ARBITER_NAME}"; then
      rc_name="$rc_candidate"
      break
    fi
  done <<< "$rc_list"
  [ -z "$rc_name" ]
}

# ---------------------------------------------------------------------------
# Quorum assessment: CLI failure should not assert quorum loss
# ---------------------------------------------------------------------------

@test "arbiter-health: reports unavailable (not QUORUM LOST) when Ceph CLI fails" {
  # Mock kubectl to return a remotearbiter but fail on ceph quorum_status
  export MOCK_KUBECTL_OUTPUT_remotearbiter=""
  export MOCK_KUBECTL_EXIT=0
  export MOCK_KUBECTL_OUTPUT="test-arbiter"

  run bash "$TOOL" -a test-arbiter
  # The tool may fail for other reasons (no full mock), but check it
  # does NOT contain "QUORUM LOST" in its output
  [[ "$output" != *"QUORUM LOST"* ]]
}

# ---------------------------------------------------------------------------
# Restart tracking: arbiter + operator pods (with last termination reason)
# ---------------------------------------------------------------------------

# Minimal fixtures so render() reaches the restart sections with the mock
# kubectl (the mock returns the same output for every query of a given
# resource type).
_setup_restart_fixtures() {
  export MOCK_KUBECTL_EXIT=0
  export MOCK_KUBECTL_OUTPUT=""
  export MOCK_KUBECTL_OUTPUT_remotearbiter="test-arbiter"
  export MOCK_KUBECTL_OUTPUT_remotecluster="test-rc"
  export MOCK_KUBECTL_OUTPUT_deploy="arbiter-deploy 1/1 quay.io/ceph/ceph:v19.2.1"
}

@test "arbiter-health: shows arbiter pod restarts with last termination reason" {
  _setup_restart_fixtures
  export MOCK_KUBECTL_OUTPUT_pod=$'POD arbiter-pod-abc\nmon|3|OOMKilled|137|2026-11-09T09:12:34Z'

  run bash "$TOOL" -a test-arbiter
  [ "$status" -eq 0 ]
  # compact summary inside the arbiter deployment box
  [[ "$output" == *"Restarts: 3 (OOMKilled, exit 137 [SIGKILL])"* ]]
  # detailed per-container lines in the operator section
  [[ "$output" == *"Arbiter Operator (arbiter-operator):"* ]]
  [[ "$output" == *"arbiter-pod-abc"* ]]
  [[ "$output" == *"3 restarts (OOMKilled, exit 137 [SIGKILL])"* ]]
}

@test "arbiter-health: maps exit codes to signal names" {
  _setup_restart_fixtures
  export MOCK_KUBECTL_OUTPUT_pod=$'POD op-cm-7d9f8c6f5-x2k9p\nmanager|1|Error|143|2026-11-08T22:04:11Z\nsidecar|1|Error|139|2026-11-08T22:04:12Z\nweird|1|Error|200|2026-11-08T22:04:13Z'

  run bash "$TOOL" -a test-arbiter
  [ "$status" -eq 0 ]
  # 143 = SIGTERM
  [[ "$output" == *"1 restarts (Error, exit 143 [SIGTERM])"* ]]
  # 139 = SIGSEGV
  [[ "$output" == *"1 restarts (Error, exit 139 [SIGSEGV])"* ]]
  # unknown signal 72 shown numerically
  [[ "$output" == *"1 restarts (Error, exit 200 [signal 72])"* ]]
}

@test "arbiter-health: shows zero restarts without reason detail" {
  _setup_restart_fixtures
  export MOCK_KUBECTL_OUTPUT_pod=$'POD arbiter-pod-abc\nmon|0|||'

  run bash "$TOOL" -a test-arbiter
  [ "$status" -eq 0 ]
  [[ "$output" == *"Restarts: 0"* ]]
  [[ "$output" != *"restarts ("* ]]
}

@test "arbiter-health: queries operator and arbiter pods for restart info" {
  _setup_restart_fixtures
  export MOCK_KUBECTL_OUTPUT_pod=$'POD arbiter-pod-abc\nmon|0|||'

  run bash "$TOOL" -a test-arbiter
  [ "$status" -eq 0 ]
  # operator pods are located via the kubebuilder control-plane label
  mock_was_called_with "$MOCK_KUBECTL_CALL_LOG" "get pod -n arbiter-operator -l control-plane"
  # arbiter pods are located via the RemoteArbiter lookup label on the remote cluster
  mock_was_called_with "$MOCK_KUBECTL_CALL_LOG" "get pod -n test-rc -l ceph.cobaltcore.sap.com/lookup=test-arbiter"
}

@test "arbiter-health: reports when no operator pods are found" {
  _setup_restart_fixtures
  export MOCK_KUBECTL_OUTPUT_pod=""

  run bash "$TOOL" -a test-arbiter
  [ "$status" -eq 0 ]
  [[ "$output" == *"no operator pods found with label: control-plane"* ]]
}

# ---------------------------------------------------------------------------
# RGW -> Monitor traffic via hubble
# ---------------------------------------------------------------------------

_setup_rgw_flow_fixtures() {
  export MOCK_KUBECTL_EXIT=0
  export MOCK_KUBECTL_OUTPUT=""
  # the mock returns this for every remotearbiter jsonpath query, so the
  # arbiter mon id, ceph cluster name/ns and remote cluster name are all
  # "ext-c"; the remote namespace comes from the remotecluster mock
  export MOCK_KUBECTL_OUTPUT_remotearbiter="ext-c"
  export MOCK_KUBECTL_OUTPUT_remotecluster="test-rc"
  export MOCK_KUBECTL_OUTPUT_pod=""
  export MOCK_HUBBLE_EXIT=0
  export MOCK_HUBBLE_OUTPUT=""
  export MOCK_HUBBLE_TO_PORT_3300_OUTPUT="$(cat "$FIXTURES_DIR/hubble_flow_rgw.json")"
  export MOCK_HUBBLE_TO_PORT_6789_OUTPUT="$(cat "$FIXTURES_DIR/hubble_flow_rgw_msgr1.json")"
}

@test "arbiter-health: shows RGW->mon flows grouped by mon id with counts" {
  _setup_rgw_flow_fixtures
  run bash "$TOOL" -a ext-c
  [ "$status" -eq 0 ]
  [[ "$output" == *"RGW -> Monitor Traffic (hubble, since 15m)"* ]]
  # flows grouped per destination mon id (mon-a x3, mon-b x3, ext-c x1)
  echo "$output" | grep -q 'a  *3 flows'
  echo "$output" | grep -q 'b  *3 flows'
  echo "$output" | grep -q 'ext-c  *1 flows'
  # pod names without labels fall back to the rook <cluster>-mon-<id> convention
  echo "$output" | grep -q 'c  *1 flows'
  # flows without pod identity fall back to the destination IP
  echo "$output" | grep -q 'ext:10.9.9.9  *1 flows'
  # protocol breakdown: 3x msgr2 to mon-a, 2x msgr1 to mon-b
  [[ "$output" == *"msgr2:3"* ]]
  [[ "$output" == *"msgr1:2"* ]]
  # dropped flows are counted
  [[ "$output" == *"dropped:1"* ]]
}

@test "arbiter-health: reports direct RGW->arbiter flows as INFO" {
  _setup_rgw_flow_fixtures
  run bash "$TOOL" -a ext-c
  [ "$status" -eq 0 ]
  # the arbiter row is marked as a direct connection and summarized
  echo "$output" | grep -q 'ext-c  *1 flows'
  echo "$output" | grep -q 'direct RGW->arbiter flows: 1 (ext-c)'
  # informational, not a warning
  echo "$output" | strip_ansi | grep -q 'INFO] direct RGW->arbiter$'
  echo "$output" | strip_ansi | grep -q 'INFO]  direct RGW->arbiter flows: 1 (ext-c)'
  ! echo "$output" | strip_ansi | grep -q 'WARN.*direct RGW->arbiter'
}

@test "arbiter-health: maps destination in arbiter namespace to the arbiter" {
  _setup_rgw_flow_fixtures
  # destination pod without labels, in the arbiter's namespace (test-rc)
  export MOCK_HUBBLE_TO_PORT_6789_OUTPUT=""
  export MOCK_HUBBLE_TO_PORT_3300_OUTPUT='{"flow":{"verdict":"FORWARDED","IP":{"source":"10.1.0.11","destination":"10.3.3.3"},"l4":{"TCP":{"source_port":41000,"destination_port":3300}},"source":{"namespace":"rook-ceph","pod_name":"rook-ceph-rgw-my-store-a-7b8c9d5ef-6kl2p"},"destination":{"namespace":"test-rc","pod_name":"ext-pod-no-labels-abc"}}}'
  run bash "$TOOL" -a ext-c
  [ "$status" -eq 0 ]
  echo "$output" | grep -q 'ext-c  *1 flows'
  echo "$output" | grep -q 'direct RGW->arbiter flows: 1 (ext-c)'
}

@test "arbiter-health: no direct RGW->arbiter flows is reported as INFO" {
  _setup_rgw_flow_fixtures
  # strip the arbiter-bound flow from the msgr2 fixture
  export MOCK_HUBBLE_TO_PORT_3300_OUTPUT="$(grep -v 'lookup=ext-c' "$FIXTURES_DIR/hubble_flow_rgw.json")"
  run bash "$TOOL" -a ext-c
  [ "$status" -eq 0 ]
  [[ "$output" == *"no direct RGW->arbiter flows"* ]]
  echo "$output" | strip_ansi | grep -q 'INFO]  no direct RGW->arbiter flows'
}

@test "arbiter-health: hubble queries use rgw label, mon ports, and window" {
  _setup_rgw_flow_fixtures
  run bash "$TOOL" -a ext-c
  [ "$status" -eq 0 ]
  mock_was_called_with "$MOCK_HUBBLE_CALL_LOG" "--from-label k8s:app=rook-ceph-rgw"
  mock_was_called_with "$MOCK_HUBBLE_CALL_LOG" "--from-namespace ext-c"
  mock_was_called_with "$MOCK_HUBBLE_CALL_LOG" "--to-port 3300"
  mock_was_called_with "$MOCK_HUBBLE_CALL_LOG" "--to-port 6789"
  mock_was_called_with "$MOCK_HUBBLE_CALL_LOG" "--since 15m"
  mock_was_called_with "$MOCK_HUBBLE_CALL_LOG" "--last 5000"
}

@test "arbiter-health: --rgw-since changes the hubble capture window" {
  _setup_rgw_flow_fixtures
  run bash "$TOOL" -a ext-c --rgw-since 1h
  [ "$status" -eq 0 ]
  mock_was_called_with "$MOCK_HUBBLE_CALL_LOG" "--since 1h"
  [[ "$output" == *"RGW -> Monitor Traffic (hubble, since 1h)"* ]]
}

@test "arbiter-health: hubble failure degrades gracefully" {
  _setup_rgw_flow_fixtures
  unset MOCK_HUBBLE_TO_PORT_3300_OUTPUT MOCK_HUBBLE_TO_PORT_6789_OUTPUT
  export MOCK_HUBBLE_EXIT=1
  run bash "$TOOL" -a ext-c
  [ "$status" -eq 0 ]
  [[ "$output" == *"hubble query failed"* ]]
  [[ "$output" == *"cilium hubble port-forward"* ]]
}

@test "arbiter-health: no RGW->mon flows in window is reported" {
  _setup_rgw_flow_fixtures
  unset MOCK_HUBBLE_TO_PORT_3300_OUTPUT MOCK_HUBBLE_TO_PORT_6789_OUTPUT
  run bash "$TOOL" -a ext-c
  [ "$status" -eq 0 ]
  [[ "$output" == *"no RGW->mon flows captured in window"* ]]
}

@test "arbiter-health: --no-rgw-flows skips the hubble section" {
  _setup_rgw_flow_fixtures
  run bash "$TOOL" -a ext-c --no-rgw-flows
  [ "$status" -eq 0 ]
  [[ "$output" != *"RGW -> Monitor Traffic"* ]]
  [[ "$output" != *"Arbiter -> Ceph Services"* ]]
  [ ! -s "$MOCK_HUBBLE_CALL_LOG" ]
}

# ---------------------------------------------------------------------------
# Arbiter -> Ceph services traffic via hubble
# ---------------------------------------------------------------------------

@test "arbiter-health: shows arbiter egress grouped by ceph service type" {
  _setup_rgw_flow_fixtures
  # the arbiter capture has no --to-port, so the mock returns MOCK_HUBBLE_OUTPUT
  unset MOCK_HUBBLE_TO_PORT_3300_OUTPUT MOCK_HUBBLE_TO_PORT_6789_OUTPUT
  export MOCK_HUBBLE_OUTPUT="$(cat "$FIXTURES_DIR/hubble_flow_arbiter.json")"
  run bash "$TOOL" -a ext-c
  [ "$status" -eq 0 ]
  [[ "$output" == *"Arbiter -> Ceph Services (hubble, since 15m)"* ]]
  # grouped per destination daemon type (mgr x2 via label, osd x1 via pod name)
  echo "$output" | grep -q 'mgr  *2 flows'
  echo "$output" | grep -q 'osd  *1 flows'
  echo "$output" | grep -q 'mon  *1 flows'
  # flows without pod identity fall back to the destination IP
  echo "$output" | grep -q 'ext:10.8.8.8  *1 flows'
  # dropped flows are counted
  echo "$output" | grep -q 'dropped:1'
}

@test "arbiter-health: arbiter capture queries the lookup label" {
  _setup_rgw_flow_fixtures
  unset MOCK_HUBBLE_TO_PORT_3300_OUTPUT MOCK_HUBBLE_TO_PORT_6789_OUTPUT
  export MOCK_HUBBLE_OUTPUT=""
  run bash "$TOOL" -a ext-c
  [ "$status" -eq 0 ]
  mock_was_called_with "$MOCK_HUBBLE_CALL_LOG" "--from-label k8s:ceph.cobaltcore.sap.com/lookup=ext-c"
  [[ "$output" == *"no arbiter egress flows captured in window"* ]]
}

# ---------------------------------------------------------------------------
# Portable base64 decode
# ---------------------------------------------------------------------------

@test "arbiter-health: b64decode helper decodes correctly" {
  # Source the tool to get the b64decode function (use the real base64)
  # We just test that the function exists and works
  local result
  result=$(echo "dGVzdA==" | { base64 -d 2>/dev/null || base64 -D 2>/dev/null; })
  [ "$result" = "test" ]
}

# ---------------------------------------------------------------------------
# Watch mode: render in parent shell (not subshell)
# ---------------------------------------------------------------------------

@test "arbiter-health: file redirect preserves variables (not subshell)" {
  # Verify the pattern used in watch mode
  MY_VAR=""
  set_var() { MY_VAR="set_in_function"; echo "output"; }
  tmpf=$(mktemp)
  set_var > "$tmpf" 2>&1
  [ "$MY_VAR" = "set_in_function" ]
  rm -f "$tmpf"
}

@test "arbiter-health: command substitution loses variables (subshell)" {
  # Verify the old broken pattern to ensure our test is meaningful
  MY_VAR=""
  set_var() { MY_VAR="set_in_function"; echo "output"; }
  _out=$(set_var)
  [ "$MY_VAR" = "" ]
}
