#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)
DEPLOYMENT="odh-dashboard"
BAD_SECRET="scenario-bad-secret"

log_step "=== Scenario: Component Misconfiguration — Setup ==="

# Continuously patch the deployment to reference a nonexistent secret
log_step "Starting background loop to inject bad secret ref into $DEPLOYMENT..."
(
  while true; do
    oc patch deployment "$DEPLOYMENT" -n "$APPS_NS" --type=json \
      -p '[{"op":"add","path":"/spec/template/spec/containers/0/envFrom","value":[{"secretRef":{"name":"'"$BAD_SECRET"'"}}]}]' 2>/dev/null || true
    sleep 3
  done
) &
LOOP_PID=$!

STATE_DIR="${XDG_RUNTIME_DIR:-/tmp}/odh-mcp-scenarios-${UID}"
mkdir -p "$STATE_DIR"
chmod 700 "$STATE_DIR"
printf '%s\n' "$LOOP_PID" > "$STATE_DIR/odh-misconfiguration-pid"
printf '%s\n' "$DEPLOYMENT" > "$STATE_DIR/odh-misconfiguration-deploy"

log_step "Background loop running (PID: $LOOP_PID)"

# Wait for pods to hit CreateContainerConfigError
log_step "Waiting for pods to fail with CreateContainerConfigError..."
elapsed=0
detected=false
while (( elapsed < 120 )); do
  waiting=$(oc get pods -n "$APPS_NS" -l "app=$DEPLOYMENT" \
    -o jsonpath='{.items[*].status.containerStatuses[*].state.waiting.reason}' 2>/dev/null || echo "")
  if echo "$waiting" | grep -q "CreateContainerConfigError"; then
    log_step "Pod is in CreateContainerConfigError state."
    detected=true
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

if [[ "$detected" == "false" ]]; then
  log_step "WARN: CreateContainerConfigError not detected within timeout."
  log_step "Current pod status:"
  oc get pods -n "$APPS_NS" -l "app=$DEPLOYMENT" --no-headers 2>/dev/null
fi

log_step "Component misconfiguration injected."
log_step "Run MCP tools to validate, then run component-misconfiguration-teardown.sh to restore."
