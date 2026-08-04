#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

STATE_DIR="${XDG_RUNTIME_DIR:-/tmp}/odh-mcp-scenarios-${UID}"
mkdir -p "$STATE_DIR"
chmod 700 "$STATE_DIR"

log_step "=== Scenario 13: Network Not Ready — Setup ==="

# Select a worker node
WORKER_NODE=$(oc get nodes -l node-role.kubernetes.io/worker -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [[ -z "$WORKER_NODE" ]]; then
  log_step "ERROR: No worker node found"
  exit 1
fi
log_step "Selected worker node: $WORKER_NODE"
printf '%s\n' "$WORKER_NODE" > "$STATE_DIR/odh-network-not-ready-node"

# Backup node
backup_resource node "$WORKER_NODE" "" "$BACKUP_DIR/13-${WORKER_NODE}.json"

# Patch node status to add NetworkUnavailable=True condition.
# Kubelet resets conditions periodically, so we run a background patcher.
log_step "Patching $WORKER_NODE with NetworkUnavailable=True (background patcher)..."

(
  while true; do
    oc patch node "$WORKER_NODE" --type=strategic --subresource=status -p '{
      "status": {
        "conditions": [
          {
            "type": "NetworkUnavailable",
            "status": "True",
            "reason": "NetworkNotReady",
            "message": "Scenario 13: simulated network failure",
            "lastHeartbeatTime": "'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'",
            "lastTransitionTime": "'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'"
          }
        ]
      }
    }' 2>/dev/null || true
    sleep 10
  done
) &

PATCHER_PID=$!
printf '%s\n' "$PATCHER_PID" > "$STATE_DIR/odh-network-not-ready-pid"
log_step "Background patcher PID: $PATCHER_PID"

# Wait for the condition to appear
sleep 5
condition=$(oc get node "$WORKER_NODE" -o jsonpath='{.status.conditions[?(@.type=="NetworkUnavailable")].status}' 2>/dev/null || echo "")
if [[ "$condition" == "True" ]]; then
  log_step "NetworkUnavailable=True condition set on $WORKER_NODE."
else
  log_step "WARN: NetworkUnavailable condition not detected (current: $condition)"
fi

log_step "Network not ready failure injected."
log_step "Run MCP tools to validate, then run network-not-ready-teardown.sh to restore."
