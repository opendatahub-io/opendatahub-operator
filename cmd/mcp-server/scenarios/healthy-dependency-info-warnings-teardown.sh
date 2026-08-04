#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

log_step "=== Scenario 29: Healthy Dependency Info Warnings — Teardown ==="

# No changes were made — this scenario uses the cluster's natural state
log_step "No cleanup needed — scenario used existing cluster state."
log_step "Scenario 29 teardown complete."
