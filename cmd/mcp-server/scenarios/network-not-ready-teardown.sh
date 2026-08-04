#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

STATE_DIR="${XDG_RUNTIME_DIR:-/tmp}/odh-mcp-scenarios-${UID}"

log_step "=== Scenario 13: Network Not Ready — Teardown ==="

# Kill the background patcher
PID_FILE="$STATE_DIR/odh-network-not-ready-pid"
if [[ -f "$PID_FILE" ]]; then
  PATCHER_PID=$(cat "$PID_FILE")
  if kill -0 "$PATCHER_PID" 2>/dev/null; then
    log_step "Killing background patcher (PID $PATCHER_PID)..."
    kill "$PATCHER_PID" 2>/dev/null || true
  fi
  rm -f "$PID_FILE"
fi

# Kubelet does NOT auto-clear NetworkUnavailable (unlike MemoryPressure),
# so we must patch it back to False manually.
NODE_FILE="$STATE_DIR/odh-network-not-ready-node"
if [[ -f "$NODE_FILE" ]]; then
  WORKER_NODE=$(cat "$NODE_FILE")
  log_step "Patching NetworkUnavailable=False on $WORKER_NODE..."
  oc patch node "$WORKER_NODE" --type=strategic --subresource=status -p '{
    "status": {
      "conditions": [
        {
          "type": "NetworkUnavailable",
          "status": "False",
          "reason": "NetworkReady",
          "message": "Scenario 13 teardown: network restored",
          "lastHeartbeatTime": "'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'",
          "lastTransitionTime": "'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'"
        }
      ]
    }
  }'
  log_step "NetworkUnavailable condition cleared on $WORKER_NODE."
  rm -f "$NODE_FILE"
fi

log_step "Network restored. Scenario 13 teardown complete."
