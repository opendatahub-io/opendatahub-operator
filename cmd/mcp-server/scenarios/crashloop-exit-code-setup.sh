#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
DEPLOYMENT="odh-dashboard"
CONTAINER="odh-dashboard"

log_step "=== Scenario 15: CrashLoop Exit Code — Setup ==="

# Scale down the ODH operator so it doesn't revert our change
log_step "Scaling down ODH operator to prevent reconciliation..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=0
sleep 5

# Backup original deployment
backup_resource deployment "$DEPLOYMENT" "$APPS_NS" "$BACKUP_DIR/15-${DEPLOYMENT}.json"

# Patch dashboard container command to /bin/false (exits immediately with code 1)
log_step "Patching $DEPLOYMENT container command to /bin/false..."
oc patch deployment "$DEPLOYMENT" -n "$APPS_NS" --type=json \
  -p '[{"op":"add","path":"/spec/template/spec/containers/0/command","value":["/bin/false"]}]'

# Wait for CrashLoopBackOff
log_step "Waiting for container to enter CrashLoopBackOff..."
elapsed=0
while (( elapsed < 120 )); do
  waiting_reason=$(oc get pods -n "$APPS_NS" -l app=odh-dashboard \
    -o jsonpath='{.items[0].status.containerStatuses[?(@.name=="'"$CONTAINER"'")].state.waiting.reason}' 2>/dev/null || echo "")
  if [[ "$waiting_reason" == "CrashLoopBackOff" ]]; then
    log_step "Container is in CrashLoopBackOff."
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

if [[ "$waiting_reason" != "CrashLoopBackOff" ]]; then
  log_step "WARN: Container did not enter CrashLoopBackOff within timeout (current: $waiting_reason)"
fi

log_step "CrashLoop exit code failure injected."
log_step "Run MCP tools to validate, then run crashloop-exit-code-teardown.sh to restore."
