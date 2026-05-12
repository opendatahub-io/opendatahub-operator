#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)

log_step "=== Scenario: Cascading Failure — Teardown ==="

log_step "Deleting deny-all NetworkPolicy..."
oc delete networkpolicy deny-all-cascade-test -n "$APPS_NS" 2>/dev/null || true

log_step "Waiting for components to recover..."
sleep 30

UNREADY=$(oc get deployments -n "$APPS_NS" -o json 2>/dev/null | \
  jq -r '.items[] | select(.status.replicas != .status.availableReplicas) | .metadata.name' || true)
if [[ -z "$UNREADY" ]]; then
  log_step "All deployments are ready."
else
  log_step "Still recovering: $UNREADY"
  log_step "Waiting additional time..."
  sleep 30
fi

log_step "Cascading failure scenario teardown complete."
