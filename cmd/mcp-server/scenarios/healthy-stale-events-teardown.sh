#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

log_step "=== Scenario 30: Healthy With Stale Events — Teardown ==="

# No cleanup needed — cluster is already healthy, stale events will expire naturally (~1 hour)
log_step "No cleanup needed — cluster is healthy, stale events will expire automatically."
log_step "Scenario 30 teardown complete."
