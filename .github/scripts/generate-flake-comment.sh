#!/usr/bin/env bash
# Generates a markdown comment from flake-report JSON and quarantine config.
#
# Usage: generate-flake-comment.sh <report.json> <quarantine.json> <output.md>

set -euo pipefail

REPORT="${1:?Usage: generate-flake-comment.sh <report.json> <quarantine.json> <output.md>}"
QCFG="${2:?}"
MD="${3:?}"

echo "## Flake Report — $(date -u '+%Y-%m-%d %H:%M UTC')" > "$MD"
echo "" >> "$MD"
printf '**Files analyzed:** %s | **Tests tracked:** %s | **Threshold:** %s%%\n' \
  "$(jq -r '.total_files' "$REPORT")" \
  "$(jq -r '.total_tests' "$REPORT")" \
  "$(jq -r '(.threshold * 100 | floor)' "$REPORT")" >> "$MD"
echo "" >> "$MD"

REG_COUNT=$(jq '.regressions | length' "$REPORT")
if [ "$REG_COUNT" -gt 0 ]; then
  echo "### Regressions ($REG_COUNT)" >> "$MD"
  echo "" >> "$MD"
  echo "| Test | Flake Rate | Failed/Total | Transition Commit |" >> "$MD"
  echo "|------|-----------|--------------|-------------------|" >> "$MD"
  jq -r '.regressions[] | "| `\(.name)` | \(.flake_rate * 100 | floor)% | \(.failed_runs)/\(.total_runs) | \(.transition_commit // "—") |"' "$REPORT" >> "$MD"
  echo "" >> "$MD"
fi

FLAKY_COUNT=$(jq '.flaky_tests | length' "$REPORT")
if [ "$FLAKY_COUNT" -gt 0 ]; then
  echo "### Flaky Tests ($FLAKY_COUNT)" >> "$MD"
  echo "" >> "$MD"
  echo "| Test | Flake Rate | Failed/Total |" >> "$MD"
  echo "|------|-----------|--------------|" >> "$MD"
  jq -r '.flaky_tests[] | "| `\(.name)` | \(.flake_rate * 100 | floor)% | \(.failed_runs)/\(.total_runs) |"' "$REPORT" >> "$MD"
  echo "" >> "$MD"
fi

Q_COUNT=$(jq '.tests | length' "$QCFG")
if [ "$Q_COUNT" -gt 0 ]; then
  echo "### Quarantined Tests ($Q_COUNT)" >> "$MD"
  echo "" >> "$MD"
  echo "| Test | Jira | Flake Rate | Re-enable After |" >> "$MD"
  echo "|------|------|-----------|-----------------|" >> "$MD"
  jq -r '.tests[] | "| `\(.name)` | \(.jira // "—") | \(.flake_rate * 100 | floor)% | \(.re_enable_after // "never") |"' "$QCFG" >> "$MD"
  echo "" >> "$MD"
fi

if [ "$REG_COUNT" -eq 0 ] && [ "$FLAKY_COUNT" -eq 0 ]; then
  echo "All tests healthy — no regressions or flaky tests detected." >> "$MD"
  echo "" >> "$MD"
fi
