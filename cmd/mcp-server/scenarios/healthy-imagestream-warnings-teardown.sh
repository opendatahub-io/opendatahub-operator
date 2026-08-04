#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)

log_step "=== Scenario 28: Healthy ImageStream Warnings — Teardown ==="

# Delete the test ImageStream if we created one
if oc get imagestream scenario-test-notebook -n "$APPS_NS" &>/dev/null; then
  log_step "Deleting test ImageStream scenario-test-notebook..."
  oc delete imagestream scenario-test-notebook -n "$APPS_NS"
fi

log_step "Scenario 28 teardown complete."
