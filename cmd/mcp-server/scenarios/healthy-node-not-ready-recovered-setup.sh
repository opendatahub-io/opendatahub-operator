#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

log_step "=== Scenario 27: Healthy Node NotReady Recovered — Setup ==="

# Pick a worker node (not master/control-plane)
WORKER_NODE=$(oc get nodes -l node-role.kubernetes.io/worker -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
if [[ -z "$WORKER_NODE" ]]; then
  log_step "ERROR: No worker node found."
  exit 1
fi
log_step "Target worker node: $WORKER_NODE"

# Create a synthetic NodeNotReady event — simulates a brief NotReady that already recovered
# We can't actually make the node NotReady safely (kubelet resets it instantly),
# so we inject the event directly to test agent behavior
NOW=$(date -u +%Y-%m-%dT%H:%M:%SZ)
log_step "Injecting synthetic NodeNotReady event for $WORKER_NODE..."
oc apply -f - <<EOF
apiVersion: v1
kind: Event
metadata:
  name: scenario-27-node-not-ready
  namespace: default
involvedObject:
  apiVersion: v1
  kind: Node
  name: $WORKER_NODE
reason: NodeNotReady
message: "Node $WORKER_NODE status is now: NodeNotReady"
type: Warning
firstTimestamp: "$NOW"
lastTimestamp: "$NOW"
count: 1
source:
  component: node-controller
EOF

# Also inject a NodeReady event right after — shows the recovery
log_step "Injecting synthetic NodeReady event (recovery)..."
oc apply -f - <<EOF
apiVersion: v1
kind: Event
metadata:
  name: scenario-27-node-ready
  namespace: default
involvedObject:
  apiVersion: v1
  kind: Node
  name: $WORKER_NODE
reason: NodeReady
message: "Node $WORKER_NODE status is now: NodeReady"
type: Normal
firstTimestamp: "$NOW"
lastTimestamp: "$NOW"
count: 1
source:
  component: node-controller
EOF

sleep 2
NODE_STATUS=$(oc get node "$WORKER_NODE" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}')
NOT_READY_EVENTS=$(oc get events -A --field-selector="reason=NodeNotReady" --no-headers 2>/dev/null | wc -l || echo "0")

log_step "Node status: Ready=$NODE_STATUS"
log_step "NodeNotReady events found: $NOT_READY_EVENTS"

# Verify all ODH pods are healthy
APPS_NS=$(discover_apps_namespace)
UNHEALTHY_PODS=$(oc get pods -n "$APPS_NS" --field-selector='status.phase!=Running,status.phase!=Succeeded' --no-headers 2>/dev/null | wc -l)
if (( UNHEALTHY_PODS == 0 )); then
  log_step "All ODH pods healthy — false-positive condition active."
else
  log_step "WARN: $UNHEALTHY_PODS unhealthy pods found."
fi

log_step "Node recovered with stale NotReady events. Agent should report 'Platform Healthy'."
log_step "Run healthy-node-not-ready-recovered-teardown.sh to clean up."
