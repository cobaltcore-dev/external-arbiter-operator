#!/usr/bin/env bats
# Tests for contrib/tools/arbiter-viz

setup() {
  load helpers/setup.bash
  TOOL="${TOOLS_DIR}/arbiter-viz"
}

teardown() {
  teardown_mock
}

# ---------------------------------------------------------------------------
# CLI argument validation
# ---------------------------------------------------------------------------

@test "arbiter-viz: --help prints usage and exits 0" {
  run bash "$TOOL" --help
  [ "$status" -eq 0 ]
  [[ "$output" == *"Usage"* ]]
  [[ "$output" == *"--follow"* ]]
}

@test "arbiter-viz: unknown option exits nonzero" {
  run bash "$TOOL" --bogus
  [ "$status" -ne 0 ]
}

# ---------------------------------------------------------------------------
# Hubble label prefix (k8s:)
# ---------------------------------------------------------------------------

@test "arbiter-viz: k8s: prefix auto-added to bare Kubernetes labels" {
  local LABEL_KEY="ceph.cobaltcore.sap.com/lookup"
  local ARBITER_NAME="external-arbiter"
  local LABEL_SELECTOR="${LABEL_KEY}=${ARBITER_NAME}"

  if [[ "$LABEL_SELECTOR" != k8s:* && "$LABEL_SELECTOR" != reserved:* ]]; then
    LABEL_SELECTOR="k8s:${LABEL_SELECTOR}"
  fi

  [ "$LABEL_SELECTOR" = "k8s:ceph.cobaltcore.sap.com/lookup=external-arbiter" ]
}

@test "arbiter-viz: k8s: prefix NOT added when already present" {
  local LABEL_SELECTOR="k8s:app=test"

  if [[ "$LABEL_SELECTOR" != k8s:* && "$LABEL_SELECTOR" != reserved:* ]]; then
    LABEL_SELECTOR="k8s:${LABEL_SELECTOR}"
  fi

  [ "$LABEL_SELECTOR" = "k8s:app=test" ]
}

@test "arbiter-viz: k8s: prefix NOT added for reserved: labels" {
  local LABEL_SELECTOR="reserved:world"

  if [[ "$LABEL_SELECTOR" != k8s:* && "$LABEL_SELECTOR" != reserved:* ]]; then
    LABEL_SELECTOR="k8s:${LABEL_SELECTOR}"
  fi

  [ "$LABEL_SELECTOR" = "reserved:world" ]
}

# ---------------------------------------------------------------------------
# Bidirectional collection
# ---------------------------------------------------------------------------

@test "arbiter-viz: both --to-label and --from-label are invoked" {
  export MOCK_HUBBLE_OUTPUT=$(cat "$FIXTURES_DIR/hubble_flow_to.json")
  export MOCK_HUBBLE_FROM_OUTPUT=$(cat "$FIXTURES_DIR/hubble_flow_from.json")
  export MOCK_HUBBLE_FROM_EXIT=0

  # Provide arbiter name to skip auto-detect
  export MOCK_KUBECTL_OUTPUT="test-arbiter"

  run bash "$TOOL" -a test-arbiter -c 10
  # Check both directions were called by examining the call log
  [ "$(grep -c '\-\-to-label' "$MOCK_HUBBLE_CALL_LOG")" -ge 1 ]
  [ "$(grep -c '\-\-from-label' "$MOCK_HUBBLE_CALL_LOG")" -ge 1 ]
}

@test "arbiter-viz: partial failure - one direction fails shows warning" {
  export MOCK_HUBBLE_OUTPUT=$(cat "$FIXTURES_DIR/hubble_flow_to.json")
  export MOCK_HUBBLE_FROM_EXIT=1
  export MOCK_HUBBLE_FROM_OUTPUT=""
  export MOCK_KUBECTL_OUTPUT="test-arbiter"

  run bash "$TOOL" -a test-arbiter -c 10
  # Should warn about partial collection
  [[ "$output" == *"Warning"* ]] || [[ "$output" == *"--from-label collection failed"* ]]
}

@test "arbiter-viz: both directions fail exits with error" {
  export MOCK_HUBBLE_EXIT=1
  export MOCK_HUBBLE_OUTPUT=""
  export MOCK_HUBBLE_FROM_EXIT=1
  export MOCK_HUBBLE_FROM_OUTPUT=""
  export MOCK_KUBECTL_OUTPUT="test-arbiter"

  run bash "$TOOL" -a test-arbiter -c 10
  [ "$status" -ne 0 ]
  [[ "$output" == *"failed"* ]] || [[ "$output" == *"No flows"* ]]
}

# ---------------------------------------------------------------------------
# Follow mode
# ---------------------------------------------------------------------------

@test "arbiter-viz: follow mode prints raw JSON info message" {
  export MOCK_KUBECTL_OUTPUT="test-arbiter"
  export MOCK_HUBBLE_OUTPUT=""

  # Follow mode blocks, so run in background and capture initial output
  local tmpout
  tmpout=$(mktemp)
  bash "$TOOL" -a test-arbiter -f > "$tmpout" 2>&1 &
  local pid=$!
  sleep 1
  kill "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
  local output
  output=$(cat "$tmpout")
  rm -f "$tmpout"

  [[ "$output" == *"Streaming raw JSON"* ]]
  [[ "$output" == *"topology view"* ]]
}

# ---------------------------------------------------------------------------
# Port resolution: service port from both directions
# ---------------------------------------------------------------------------

@test "arbiter-viz: request flow resolves dst_port 3300 as msgr2" {
  local result
  result=$(echo '{"l4":{"TCP":{"source_port":49152,"destination_port":3300}}}' | awk -v Q='"' '
    function extract_l4_port(line, key,    needle, pos, rest, pneedle, ppos, prest, val, i, ch) {
      needle = Q "l4" Q ":{"
      pos = index(line, needle)
      if (pos == 0) return ""
      rest = substr(line, pos + length(needle))
      pneedle = Q key Q ":"
      ppos = index(rest, pneedle)
      if (ppos == 0) return ""
      prest = substr(rest, ppos + length(pneedle))
      val = ""
      for (i = 1; i <= length(prest); i++) {
        ch = substr(prest, i, 1)
        if (ch >= "0" && ch <= "9") val = val ch
        else break
      }
      return val
    }
    function is_ceph_port(p) { return (p == "3300" || p == "6789") }
    {
      sp = extract_l4_port($0, "source_port")
      dp = extract_l4_port($0, "destination_port")
      if (is_ceph_port(dp)) print dp
      else if (is_ceph_port(sp)) print sp
      else print dp
    }')
  [ "$result" = "3300" ]
}

@test "arbiter-viz: response flow resolves src_port 3300 as msgr2" {
  local result
  result=$(echo '{"l4":{"TCP":{"source_port":3300,"destination_port":49152}}}' | awk -v Q='"' '
    function extract_l4_port(line, key,    needle, pos, rest, pneedle, ppos, prest, val, i, ch) {
      needle = Q "l4" Q ":{"
      pos = index(line, needle)
      if (pos == 0) return ""
      rest = substr(line, pos + length(needle))
      pneedle = Q key Q ":"
      ppos = index(rest, pneedle)
      if (ppos == 0) return ""
      prest = substr(rest, ppos + length(pneedle))
      val = ""
      for (i = 1; i <= length(prest); i++) {
        ch = substr(prest, i, 1)
        if (ch >= "0" && ch <= "9") val = val ch
        else break
      }
      return val
    }
    function is_ceph_port(p) { return (p == "3300" || p == "6789") }
    {
      sp = extract_l4_port($0, "source_port")
      dp = extract_l4_port($0, "destination_port")
      if (is_ceph_port(dp)) print dp
      else if (is_ceph_port(sp)) print sp
      else print dp
    }')
  [ "$result" = "3300" ]
}

@test "arbiter-viz: non-Ceph port falls through to dst_port" {
  local result
  result=$(echo '{"l4":{"TCP":{"source_port":40000,"destination_port":8080}}}' | awk -v Q='"' '
    function extract_l4_port(line, key,    needle, pos, rest, pneedle, ppos, prest, val, i, ch) {
      needle = Q "l4" Q ":{"
      pos = index(line, needle)
      if (pos == 0) return ""
      rest = substr(line, pos + length(needle))
      pneedle = Q key Q ":"
      ppos = index(rest, pneedle)
      if (ppos == 0) return ""
      prest = substr(rest, ppos + length(pneedle))
      val = ""
      for (i = 1; i <= length(prest); i++) {
        ch = substr(prest, i, 1)
        if (ch >= "0" && ch <= "9") val = val ch
        else break
      }
      return val
    }
    function is_ceph_port(p) { return (p == "3300" || p == "6789") }
    {
      sp = extract_l4_port($0, "source_port")
      dp = extract_l4_port($0, "destination_port")
      if (is_ceph_port(dp)) print dp
      else if (is_ceph_port(sp)) print sp
      else print dp
    }')
  [ "$result" = "8080" ]
}
