#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

log_step "=== Scenario: Node Pressure — Teardown ==="

STATE_DIR="${XDG_RUNTIME_DIR:-/tmp}/odh-mcp-scenarios-${UID}"

# Kill the background patcher
if [[ -f "$STATE_DIR/odh-node-pressure-pid" ]]; then
  PATCHER_PID=$(cat "$STATE_DIR/odh-node-pressure-pid")
  if [[ "$PATCHER_PID" =~ ^[0-9]+$ ]] && (( PATCHER_PID > 1 )); then
    log_step "Stopping background patcher (PID: $PATCHER_PID)..."
    kill -- "$PATCHER_PID" 2>/dev/null || true
  else
    log_step "WARN: Invalid PID in state file, skipping kill."
  fi
  rm -f "$STATE_DIR/odh-node-pressure-pid"
else
  log_step "No background patcher PID file found, skipping."
fi

# The kubelet will auto-restore correct conditions within ~10-40 seconds.
# Optionally patch it back to False immediately.
if [[ -f "$STATE_DIR/odh-node-pressure-node" ]]; then
  NODE=$(cat "$STATE_DIR/odh-node-pressure-node")
  if ! [[ "$NODE" =~ ^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$ ]]; then
    log_step "ERROR: Invalid node name format, skipping restore."
    rm -f "$STATE_DIR/odh-node-pressure-node"
    exit 1
  fi
  log_step "Restoring MemoryPressure=False on node $NODE..."

  RESTORE_PATCH='{
    "status": {
      "conditions": [
        {
          "type": "MemoryPressure",
          "status": "False",
          "lastHeartbeatTime": "2026-01-01T00:00:00Z",
          "lastTransitionTime": "2026-01-01T00:00:00Z",
          "reason": "KubeletHasSufficientMemory",
          "message": "kubelet has sufficient memory available"
        }
      ]
    }
  }'
  oc patch node "$NODE" --type=strategic --subresource=status -p "$RESTORE_PATCH" 2>/dev/null || true

  rm -f "$STATE_DIR/odh-node-pressure-node"
  log_step "Node condition restored (kubelet will also auto-fix within seconds)."
else
  log_step "No node file found, kubelet will auto-restore conditions."
fi

log_step "Node pressure scenario teardown complete."
