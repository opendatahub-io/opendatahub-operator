#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)
TARGET_DEPLOY="model-registry-operator-controller-manager"

log_step "=== Scenario: Partial Failure — Setup ==="

# Continuously scale the component to 0 to overcome operator reconciliation
log_step "Starting background loop to keep $TARGET_DEPLOY scaled to 0..."
(
  fail_count=0
  while true; do
    if ! oc scale deployment "$TARGET_DEPLOY" -n "$APPS_NS" --replicas=0 2>/dev/null; then
      (( fail_count++ ))
      echo "[$(date '+%Y-%m-%d %H:%M:%S')] WARN: oc scale failed ($fail_count consecutive)" >&2
      if (( fail_count >= 10 )); then
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] ERROR: Too many consecutive failures, aborting loop" >&2
        break
      fi
    else
      fail_count=0
    fi
    sleep 3
  done
) &
LOOP_PID=$!

trap 'kill $LOOP_PID 2>/dev/null || true' EXIT

STATE_DIR="${XDG_RUNTIME_DIR:-/tmp}/odh-mcp-scenarios-${UID}"
mkdir -p "$STATE_DIR"
chmod 700 "$STATE_DIR"
printf '%s\n' "$LOOP_PID" > "$STATE_DIR/odh-partial-failure-pid"
printf '%s\n' "$TARGET_DEPLOY" > "$STATE_DIR/odh-partial-failure-deploy"

trap - EXIT

log_step "Background loop running (PID: $LOOP_PID)"

# Wait for DSC to register the unhealthy state
log_step "Waiting for DSC to detect the component failure..."
sleep 15

log_step "Verifying partial failure state..."
READY=$(oc get deployment "$TARGET_DEPLOY" -n "$APPS_NS" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
log_step "$TARGET_DEPLOY ready replicas: ${READY:-0}"

log_step "Partial failure scenario setup complete. Operator is running but one component is kept down."
