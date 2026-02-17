#!/usr/bin/env bash
# Generates a test summary from go test -v output (pass/fail counts, failed details, coverage).
# Reusable by any integration test job that produces test.log, exitcode.txt, and optionally coverage.out.
#
# In CI: GITHUB_STEP_SUMMARY is set; output is appended there and shown in the Job summary.#
# Optional env:
#   SUMMARY_TITLE - Section heading (default: "Integration Tests")
set +e

EXITCODE=$(cat exitcode.txt 2>/dev/null || echo 0)
TITLE="${SUMMARY_TITLE:-Integration Tests}"
{
  echo "## $TITLE"
  if [ "$EXITCODE" = "0" ]; then echo "**Overall: PASSED**"; else echo "**Overall: FAILED**"; fi
  echo ""

  if [ -f test.log ]; then
    PASSED=$(grep -cE -- "--- PASS:" test.log 2>/dev/null) || PASSED=0
    FAILED=$(grep -cE -- "--- FAIL:" test.log 2>/dev/null) || FAILED=0
    PASSED=${PASSED:-0}
    FAILED=${FAILED:-0}
    echo "| Result | Count |"
    echo "|--------|-------|"
    echo "| Passed | $PASSED |"
    echo "| Failed | $FAILED |"
    echo ""

    if [ "$FAILED" -gt 0 ] 2>/dev/null; then
      echo "### Failed tests"
      echo "<details>"
      echo "<summary>Click to expand failure details</summary>"
      echo ""
      echo '```'
      grep -A 25 -- "--- FAIL:" test.log || true
      echo '```'
      echo ""
      echo "</details>"
    fi
  fi

  if [ -f coverage.out ]; then
    echo "### Coverage"
    echo "<details>"
    echo "<summary>Click to expand</summary>"
    echo ""
    echo '```'
    go tool cover -func=coverage.out
    echo '```'
    echo "</details>"
  fi
} | if [ -n "$GITHUB_STEP_SUMMARY" ]; then tee -a "$GITHUB_STEP_SUMMARY"; else cat; fi

exit "$EXITCODE"
