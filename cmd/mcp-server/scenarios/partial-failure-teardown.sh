#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)

log_step "=== Scenario: Partial Failure — Teardown ==="

STATE_DIR="${XDG_RUNTIME_DIR:-/tmp}/odh-mcp-scenarios-${UID}"

# Kill the background loop
if [[ -f "$STATE_DIR/odh-partial-failure-pid" ]]; then
  LOOP_PID=$(cat "$STATE_DIR/odh-partial-failure-pid")
  if [[ "$LOOP_PID" =~ ^[0-9]+$ ]] && (( LOOP_PID > 1 )); then
    log_step "Stopping background loop (PID: $LOOP_PID)..."
    kill -- "$LOOP_PID" 2>/dev/null || true
  else
    log_step "WARN: Invalid PID in state file, skipping kill."
  fi
  rm -f "$STATE_DIR/odh-partial-failure-pid"
else
  log_step "No background loop PID file found, skipping."
fi

# Scale the component back up and let operator reconcile
if [[ -f "$STATE_DIR/odh-partial-failure-deploy" ]]; then
  TARGET_DEPLOY=$(cat "$STATE_DIR/odh-partial-failure-deploy")
  if ! [[ "$TARGET_DEPLOY" =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]]; then
    log_step "ERROR: Invalid deployment name, skipping scale."
    rm -f "$STATE_DIR/odh-partial-failure-deploy"
    exit 1
  fi
  log_step "Scaling $TARGET_DEPLOY back to 1 replica..."
  oc scale deployment "$TARGET_DEPLOY" -n "$APPS_NS" --replicas=1

  wait_for_deployment_ready "$APPS_NS" "$TARGET_DEPLOY" "120s"
  rm -f "$STATE_DIR/odh-partial-failure-deploy"
else
  log_step "No deploy file found, operator will reconcile automatically."
fi

log_step "Partial failure scenario teardown complete."
