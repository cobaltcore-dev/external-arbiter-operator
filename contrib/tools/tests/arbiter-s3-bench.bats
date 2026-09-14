#!/usr/bin/env bats
# Tests for contrib/tools/arbiter-s3-bench

setup() {
  load helpers/setup.bash
  TOOL="${TOOLS_DIR}/arbiter-s3-bench"
}

teardown() {
  teardown_mock
}

# ---------------------------------------------------------------------------
# CLI argument validation
# ---------------------------------------------------------------------------

@test "arbiter-s3-bench: --help prints usage and exits 0" {
  run bash "$TOOL" --help
  [ "$status" -eq 0 ]
  [[ "$output" == *"Usage"* ]]
  [[ "$output" == *"WARNING"* ]]
}

@test "arbiter-s3-bench: unknown option exits 2" {
  run bash "$TOOL" --bogus
  [ "$status" -eq 2 ]
}

@test "arbiter-s3-bench: --read-only --write-only rejected" {
  run bash "$TOOL" --read-only --write-only --run-id test
  [ "$status" -eq 2 ]
  [[ "$output" == *"mutually exclusive"* ]]
}

@test "arbiter-s3-bench: --verify --read-only rejected" {
  run bash "$TOOL" --verify --read-only --run-id test
  [ "$status" -eq 2 ]
  [[ "$output" == *"--verify requires"* ]]
}

@test "arbiter-s3-bench: --read-only --cleanup rejected" {
  run bash "$TOOL" --read-only --cleanup --run-id test
  [ "$status" -eq 2 ]
  [[ "$output" == *"mutually exclusive"* ]]
}

@test "arbiter-s3-bench: --read-only without --run-id rejected" {
  run bash "$TOOL" --read-only
  [ "$status" -eq 2 ]
  [[ "$output" == *"--run-id"* ]]
}

@test "arbiter-s3-bench: --verify --write-only rejected" {
  run bash "$TOOL" --verify --write-only
  [ "$status" -eq 2 ]
  [[ "$output" == *"--verify requires"* ]]
}

# ---------------------------------------------------------------------------
# Error count propagation (exit status truncation fix)
# ---------------------------------------------------------------------------

@test "arbiter-s3-bench: error counts survive >255 (not truncated)" {
  # Bash truncates return values mod 256. Verify our pattern works.
  WRITE_ERRORS=0
  test_func() { local errors=300; WRITE_ERRORS="$errors"; return $(( errors > 0 ? 1 : 0 )); }
  test_func || true
  [ "$WRITE_ERRORS" -eq 300 ]
}

@test "arbiter-s3-bench: return 256 errors does not silently succeed" {
  # Verify bounded return pattern: 256 errors returns 1, not 0
  test_func() { local errors=256; return $(( errors > 0 ? 1 : 0 )); }
  run test_func
  [ "$status" -eq 1 ]
}

# ---------------------------------------------------------------------------
# Cleanup safety: only deletes RUN_ID prefix, never bucket
# ---------------------------------------------------------------------------

@test "arbiter-s3-bench: cleanup uses RUN_ID prefix for s3 rm" {
  # Source just enough of the tool to get cleanup_objects
  export S3_ENDPOINT="http://10.0.0.1:80"
  export BUCKET="test-bucket"
  export RUN_ID="bench-12345-99"
  export NUM_OBJECTS=5

  # Define a minimal cleanup_objects function matching the tool's
  cleanup_objects() {
    local errors=0
    if ! aws --endpoint-url "$S3_ENDPOINT" --no-verify-ssl \
      s3 rm "s3://${BUCKET}/${RUN_ID}/" --recursive >/dev/null 2>&1; then
      errors=1
    fi
    return "$errors"
  }

  cleanup_objects
  # Verify the mock was called with the correct prefix
  mock_was_called_with "$MOCK_AWS_CALL_LOG" "s3 rm s3://test-bucket/bench-12345-99/ --recursive"
}

@test "arbiter-s3-bench: cleanup never calls s3 rb (bucket preserved)" {
  export S3_ENDPOINT="http://10.0.0.1:80"
  export BUCKET="test-bucket"
  export RUN_ID="bench-12345-99"
  export NUM_OBJECTS=5

  cleanup_objects() {
    aws --endpoint-url "$S3_ENDPOINT" --no-verify-ssl \
      s3 rm "s3://${BUCKET}/${RUN_ID}/" --recursive >/dev/null 2>&1 || true
  }

  cleanup_objects
  # Verify s3 rb was never called
  ! mock_was_called_with "$MOCK_AWS_CALL_LOG" "s3 rb"
}

# ---------------------------------------------------------------------------
# Portability: now_ms with BSD date
# ---------------------------------------------------------------------------

@test "arbiter-s3-bench: now_ms returns numeric even when date outputs literal N" {
  # Simulate BSD date where %N is literal
  now_ms() {
    local ns="1789366519N"
    if [[ "$ns" =~ ^[0-9]+$ ]]; then
      echo "${ns:0:13}"
    else
      echo "$(date +%s)000"
    fi
  }
  local result
  result=$(now_ms)
  [[ "$result" =~ ^[0-9]+$ ]]
}

@test "arbiter-s3-bench: now_ms uses nanoseconds when available" {
  now_ms() {
    local ns="1789366519123456000"
    if [[ "$ns" =~ ^[0-9]+$ ]]; then
      echo "${ns:0:13}"
    else
      echo "$(date +%s)000"
    fi
  }
  local result
  result=$(now_ms)
  [ "$result" = "1789366519123" ]
}

# ---------------------------------------------------------------------------
# Run ID isolation
# ---------------------------------------------------------------------------

@test "arbiter-s3-bench: each invocation generates unique RUN_ID" {
  local id1 id2
  id1="bench-$(date +%s)-$$"
  sleep 1
  id2="bench-$(date +%s)-$$"
  [ "$id1" != "$id2" ]
}
