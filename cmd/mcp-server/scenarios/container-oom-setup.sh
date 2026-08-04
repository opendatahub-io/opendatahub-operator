#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
DEPLOYMENT="odh-dashboard"

log_step "=== Scenario 11: Container OOMKilled — Setup ==="

# Scale down the ODH operator so it doesn't revert our resource change
log_step "Scaling down ODH operator to prevent reconciliation..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=0
sleep 5

# Backup original deployment
backup_resource deployment "$DEPLOYMENT" "$APPS_NS" "$BACKUP_DIR/11-${DEPLOYMENT}.json"

# Patch dashboard container memory request and limit to 1Mi to trigger OOMKill
log_step "Patching $DEPLOYMENT memory request+limit to 1Mi to trigger OOMKill..."
oc patch deployment "$DEPLOYMENT" -n "$APPS_NS" --type=json \
  -p '[{"op":"replace","path":"/spec/template/spec/containers/0/resources/requests/memory","value":"1Mi"},{"op":"replace","path":"/spec/template/spec/containers/0/resources/limits/memory","value":"1Mi"}]'

# Wait for OOMKilled
log_step "Waiting for container to be OOMKilled..."
elapsed=0
while (( elapsed < 120 )); do
  terminated_reason=$(oc get pods -n "$APPS_NS" -l app=odh-dashboard \
    -o jsonpath='{.items[0].status.containerStatuses[?(@.name=="odh-dashboard")].lastState.terminated.reason}' 2>/dev/null || echo "")
  if [[ "$terminated_reason" == "OOMKilled" ]]; then
    log_step "Container was OOMKilled."
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

if [[ "$terminated_reason" != "OOMKilled" ]]; then
  log_step "WARN: Container was not OOMKilled within timeout (current: $terminated_reason)"
fi

log_step "Container OOM failure injected."
log_step "Run MCP tools to validate, then run container-oom-teardown.sh to restore."
