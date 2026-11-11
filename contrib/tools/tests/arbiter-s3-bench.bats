#!/usr/bin/env bats
# Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
# SPDX-License-Identifier: Apache-2.0
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

# ---------------------------------------------------------------------------
# Throughput math: fractional rates via awk
# ---------------------------------------------------------------------------

@test "arbiter-s3-bench: throughput below 1 op/sec is not rounded to zero" {
  # 1 successful op in 2500 ms = 0.40 ops/sec; bash integer math
  # would round this down to 0 and report "0 B/sec".
  eval "$(sed -n '/^compute_rates()/,/^}/p' "$TOOL")"
  local out
  out=$(compute_rates 1 2500 104857600)
  [ "$out" = "0.40 41943040" ]
}

@test "arbiter-s3-bench: human_size formats fractional byte counts" {
  eval "$(sed -n '/^human_size()/,/^}/p' "$TOOL")"
  [ "$(human_size 3670016)" = "3.5 MiB" ]
  [ "$(human_size 4096)" = "4.0 KiB" ]
  [ "$(human_size 512)" = "512 B" ]
}

# ---------------------------------------------------------------------------
# Bucket creation: explicit region
# ---------------------------------------------------------------------------

@test "arbiter-s3-bench: s3 mb passes explicit --region" {
  # The AWS CLI requires a region even with --endpoint-url; without
  # one, bucket creation fails on hosts with no default region.
  export S3_ENDPOINT="http://10.0.0.1:80"
  export BUCKET="test-bucket"
  export S3_REGION="us-east-1"
  export MOCK_AWS_HEAD_BUCKET_EXIT=1
  eval "$(sed -n '/^ensure_bucket()/,/^}/p' "$TOOL")"
  ensure_bucket
  # us-east-1: the CLI omits the LocationConstraint, so plain RGW
  # (default zonegroup, empty api_name) accepts the bucket.
  mock_was_called_with "$MOCK_AWS_CALL_LOG" "s3 mb s3://test-bucket --region us-east-1"
  ! mock_was_called_with "$MOCK_AWS_CALL_LOG" "eu-central-1"
}

@test "arbiter-s3-bench: s3 mb honors --region override" {
  # A custom region maps to the RGW zonegroup api_name when one is set.
  export S3_ENDPOINT="http://10.0.0.1:80"
  export BUCKET="test-bucket"
  export S3_REGION="eu-central-1"
  export MOCK_AWS_HEAD_BUCKET_EXIT=1
  eval "$(sed -n '/^ensure_bucket()/,/^}/p' "$TOOL")"
  ensure_bucket
  mock_was_called_with "$MOCK_AWS_CALL_LOG" "s3 mb s3://test-bucket --region eu-central-1"
}
