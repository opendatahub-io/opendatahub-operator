#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

log_step "=== Scenario: Node Pressure — Setup ==="

# Pick a worker node
NODE=$(oc get nodes -l node-role.kubernetes.io/worker -o jsonpath='{.items[0].metadata.name}')
if [[ -z "$NODE" ]]; then
  log_step "ERROR: No worker node found"
  exit 1
fi

log_step "Selected worker node: $NODE"

PATCH_JSON='{
  "status": {
    "conditions": [
      {
        "type": "MemoryPressure",
        "status": "True",
        "lastHeartbeatTime": "2026-01-01T00:00:00Z",
        "lastTransitionTime": "2026-01-01T00:00:00Z",
        "reason": "SimulatedPressure",
        "message": "Simulated memory pressure for ODH diagnostic testing"
      }
    ]
  }
}'

# Apply the patch once to verify it works
log_step "Patching node $NODE to simulate MemoryPressure..."
oc patch node "$NODE" --type=strategic --subresource=status -p "$PATCH_JSON"

# Start a background loop to keep re-applying the patch (kubelet resets conditions every ~10-40s)
log_step "Starting background patcher to maintain simulated pressure..."
(
  while true; do
    oc patch node "$NODE" --type=strategic --subresource=status -p "$PATCH_JSON" 2>/dev/null || true
    sleep 2
  done
) &
PATCHER_PID=$!

trap 'kill $PATCHER_PID 2>/dev/null || true' EXIT

# Save state for teardown
STATE_DIR="${XDG_RUNTIME_DIR:-/tmp}/odh-mcp-scenarios-${UID}"
mkdir -p "$STATE_DIR"
chmod 700 "$STATE_DIR"
printf '%s\n' "$NODE" > "$STATE_DIR/odh-node-pressure-node"
printf '%s\n' "$PATCHER_PID" > "$STATE_DIR/odh-node-pressure-pid"

trap - EXIT

log_step "Background patcher running (PID: $PATCHER_PID)"

# Verify the condition is set
sleep 2
PRESSURE=$(oc get node "$NODE" -o jsonpath='{.status.conditions[?(@.type=="MemoryPressure")].status}')
log_step "Node $NODE MemoryPressure condition: $PRESSURE"

if [[ "$PRESSURE" == "True" ]]; then
  log_step "Node pressure scenario setup complete."
else
  log_step "WARNING: MemoryPressure not showing as True — kubelet may have already reset it. Background patcher will keep trying."
fi
