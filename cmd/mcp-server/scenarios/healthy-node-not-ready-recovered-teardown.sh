#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

log_step "=== Scenario 27: Healthy Node NotReady Recovered — Teardown ==="

# Delete synthetic events
log_step "Deleting synthetic events..."
oc delete event scenario-27-node-not-ready -n default 2>/dev/null || true
oc delete event scenario-27-node-ready -n default 2>/dev/null || true

log_step "Scenario 27 teardown complete."
