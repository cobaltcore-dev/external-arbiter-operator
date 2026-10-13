# Common bats test setup for arbiter tool tests.
# Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
# SPDX-License-Identifier: Apache-2.0
# Source this from each .bats file's setup() function.

TOOLS_DIR="$(cd "$(dirname "${BATS_TEST_FILENAME}")/.." && pwd)"
TESTS_DIR="$(cd "$(dirname "${BATS_TEST_FILENAME}")" && pwd)"
MOCK_DIR="${TESTS_DIR}/helpers/mock-commands"
FIXTURES_DIR="${TESTS_DIR}/fixtures"

# Prepend mock commands to PATH so tools call mocks instead of real binaries
export PATH="${MOCK_DIR}:${PATH}"

# Directory for mock call logs and state
export MOCK_STATE_DIR
MOCK_STATE_DIR="$(mktemp -d)"

# Reset mock state
export MOCK_KUBECTL_EXIT=0
export MOCK_KUBECTL_OUTPUT=""
export MOCK_KUBECTL_CALL_LOG="${MOCK_STATE_DIR}/kubectl.log"
export MOCK_AWS_EXIT=0
export MOCK_AWS_CALL_LOG="${MOCK_STATE_DIR}/aws.log"
export MOCK_HUBBLE_EXIT=0
export MOCK_HUBBLE_OUTPUT=""
export MOCK_HUBBLE_CALL_LOG="${MOCK_STATE_DIR}/hubble.log"
export MOCK_HUBBLE_FROM_EXIT=""
export MOCK_HUBBLE_FROM_OUTPUT=""
export MOCK_BASE64_MODE="gnu"
export MOCK_DATE_NS_SUPPORT="true"

: > "$MOCK_KUBECTL_CALL_LOG"
: > "$MOCK_AWS_CALL_LOG"
: > "$MOCK_HUBBLE_CALL_LOG"

# Clean up mock state dir on teardown
teardown_mock() {
  rm -rf "$MOCK_STATE_DIR"
}

# Helper: count lines in a call log matching a pattern
mock_called_with() {
  local log="$1" pattern="$2"
  grep -c "$pattern" "$log" 2>/dev/null || echo 0
}

# Helper: check if call log contains a pattern
mock_was_called_with() {
  local log="$1" pattern="$2"
  grep -q "$pattern" "$log" 2>/dev/null
}
