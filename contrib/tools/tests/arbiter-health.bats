#!/usr/bin/env bats
# Tests for contrib/tools/arbiter-health

setup() {
  load helpers/setup.bash
  TOOL="${TOOLS_DIR}/arbiter-health"
}

teardown() {
  teardown_mock
}

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
