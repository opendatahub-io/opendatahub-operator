#!/bin/bash
# Tests for validate_rhai_helm_config function

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEST_TMPDIR=$(mktemp -d)
trap 'rm -rf "$TEST_TMPDIR"' EXIT


source <(sed -n '/^validate_rhai_helm_config()/,/^}/p' \
    "$SCRIPT_DIR/validate-related-images.sh")

# Test counter
TESTS_RUN=0
TESTS_PASSED=0
TESTS_FAILED=0

test_case() {
    local name="$1"
    TESTS_RUN=$((TESTS_RUN + 1))
    echo "Test $TESTS_RUN: $name"
}

assert_exit_code() {
    local expected="$1"
    local actual="$2"
    if [ "$expected" -eq "$actual" ]; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        echo "  ✓ Exit code: $actual"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        echo "  ✗ Expected exit code $expected, got $actual"
        return 1
    fi
}

assert_output_contains() {
    local expected="$1"
    local output="$2"
    if echo "$output" | grep -q "$expected"; then
        TESTS_PASSED=$((TESTS_PASSED + 1))
        echo "  ✓ Output contains: '$expected'"
    else
        TESTS_FAILED=$((TESTS_FAILED + 1))
        echo "  ✗ Output does not contain: '$expected'"
        echo "    Actual output: $output"
        return 1
    fi
}

# Test 1: Empty RHAI_HELM_CONFIG with non-empty RHAI_HELM_COMPONENTS should fail
test_case "Empty config with required components should fail"
WORKDIR=$(mktemp -d)
echo "kserve" > "$WORKDIR/rhai-helm-components.txt"
touch "$WORKDIR/rhai-helm-config.txt"

RHAI_HELM_COMPONENTS="$WORKDIR/rhai-helm-components.txt"
RHAI_HELM_CONFIG="$WORKDIR/rhai-helm-config.txt"
exit_code=0
output=$(validate_rhai_helm_config 2>&1) || exit_code=$?

assert_exit_code 1 "$exit_code"
assert_output_contains "RHAI Helm chart has no relatedImages" "$output"
assert_output_contains "kserve" "$output"
rm -rf "$WORKDIR"

# Test 2: Non-empty RHAI_HELM_CONFIG with non-empty RHAI_HELM_COMPONENTS should succeed
test_case "Non-empty config with required components should succeed"
WORKDIR=$(mktemp -d)
echo "kserve" > "$WORKDIR/rhai-helm-components.txt"
echo "RELATED_IMAGE_KSERVE" > "$WORKDIR/rhai-helm-config.txt"

RHAI_HELM_COMPONENTS="$WORKDIR/rhai-helm-components.txt"
RHAI_HELM_CONFIG="$WORKDIR/rhai-helm-config.txt"
exit_code=0
output=$(validate_rhai_helm_config 2>&1) || exit_code=$?

assert_exit_code 0 "$exit_code"
rm -rf "$WORKDIR"

# Test 3: Empty RHAI_HELM_CONFIG with empty RHAI_HELM_COMPONENTS should succeed
test_case "Empty config with no required components should succeed"
WORKDIR=$(mktemp -d)
touch "$WORKDIR/rhai-helm-components.txt"
touch "$WORKDIR/rhai-helm-config.txt"

RHAI_HELM_COMPONENTS="$WORKDIR/rhai-helm-components.txt"
RHAI_HELM_CONFIG="$WORKDIR/rhai-helm-config.txt"
exit_code=0
output=$(validate_rhai_helm_config 2>&1) || exit_code=$?

assert_exit_code 0 "$exit_code"
rm -rf "$WORKDIR"

# Test 4: Multiple components with missing images should fail and list all
test_case "Multiple components with missing images should list all"
WORKDIR=$(mktemp -d)
printf "kserve\nkserve-qpext\nray" > "$WORKDIR/rhai-helm-components.txt"
touch "$WORKDIR/rhai-helm-config.txt"

RHAI_HELM_COMPONENTS="$WORKDIR/rhai-helm-components.txt"
RHAI_HELM_CONFIG="$WORKDIR/rhai-helm-config.txt"
exit_code=0
output=$(validate_rhai_helm_config 2>&1) || exit_code=$?

assert_exit_code 1 "$exit_code"
assert_output_contains "kserve" "$output"
assert_output_contains "kserve-qpext" "$output"
assert_output_contains "ray" "$output"
rm -rf "$WORKDIR"

# Print summary
echo ""
echo "================================"
echo "Tests run:    $TESTS_RUN"
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"
echo "================================"

if [ "$TESTS_FAILED" -gt 0 ]; then
    exit 1
fi
