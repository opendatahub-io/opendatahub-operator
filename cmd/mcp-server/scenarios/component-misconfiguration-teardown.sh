#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)

log_step "=== Scenario: Component Misconfiguration — Teardown ==="

STATE_DIR="${XDG_RUNTIME_DIR:-/tmp}/odh-mcp-scenarios-${UID}"

# Kill the background loop
if [[ -f ${STATE_DIR}/odh-misconfiguration-pid ]]; then
  LOOP_PID=$(cat ${STATE_DIR}/odh-misconfiguration-pid)
  log_step "Stopping background loop (PID: $LOOP_PID)..."
  if [[ "$LOOP_PID" =~ ^[0-9]+$ ]] && (( LOOP_PID > 1 )); then
    kill -- "$LOOP_PID" 2>/dev/null || true
  else
    log_step "WARN: Invalid PID in state file, skipping kill."
  fi
  rm -f ${STATE_DIR}/odh-misconfiguration-pid
else
  log_step "No background loop PID file found, skipping."
fi

DEPLOYMENT="odh-dashboard"
if [[ -f ${STATE_DIR}/odh-misconfiguration-deploy ]]; then
  DEPLOYMENT=$(cat ${STATE_DIR}/odh-misconfiguration-deploy)
  rm -f ${STATE_DIR}/odh-misconfiguration-deploy
fi

# Rollback the deployment to the last working revision
log_step "Rolling back $DEPLOYMENT to previous revision..."
oc rollout undo deployment/"$DEPLOYMENT" -n "$APPS_NS"

# Delete any stuck pods from the bad revision
log_step "Cleaning up stuck pods..."
oc delete pod -n "$APPS_NS" -l "app=$DEPLOYMENT" --field-selector=status.phase!=Running --ignore-not-found 2>/dev/null || true

wait_for_deployment_ready "$APPS_NS" "$DEPLOYMENT" "180s"

log_step "Component misconfiguration scenario teardown complete."
