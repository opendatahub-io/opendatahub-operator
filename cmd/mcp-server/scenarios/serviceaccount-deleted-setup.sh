#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
COMPONENT_DEPLOY="mlflow-operator-controller-manager"
SA_NAME="mlflow-operator-controller-manager"

log_step "=== Scenario 24: ServiceAccount Deleted (RHOAIENG-47806) — Setup ==="

# Scale down the ODH operator so it doesn't recreate the ServiceAccount
log_step "Scaling down ODH operator to prevent reconciliation..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=0
while [[ "$(oc get deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" -o jsonpath='{.status.readyReplicas}' 2>/dev/null)" != "0" ]] \
  && [[ -n "$(oc get deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" -o jsonpath='{.status.readyReplicas}' 2>/dev/null)" ]]; do
  sleep 2
done

# Backup the ServiceAccount before deletion
log_step "Backing up ServiceAccount $SA_NAME..."
oc get serviceaccount "$SA_NAME" -n "$APPS_NS" -o json > "$BACKUP_DIR/24-${SA_NAME}.json"

# Delete the ServiceAccount
log_step "Deleting ServiceAccount $SA_NAME..."
oc delete serviceaccount "$SA_NAME" -n "$APPS_NS"

# Restart the component so the new pod tries to mount the missing SA token
log_step "Restarting $COMPONENT_DEPLOY..."
oc rollout restart deployment "$COMPONENT_DEPLOY" -n "$APPS_NS"

# Wait for CreateContainerError
log_step "Waiting for CreateContainerError on $COMPONENT_DEPLOY..."
elapsed=0
while (( elapsed < 120 )); do
  status=$(oc get pods -n "$APPS_NS" -o json | jq -r --arg deploy "$COMPONENT_DEPLOY" \
    '.items[] | select(.metadata.name | startswith($deploy)) | .status.containerStatuses[]? | .state.waiting.reason // ""' 2>/dev/null)
  if echo "$status" | grep -q "CreateContainerError\|CreateContainerConfigError"; then
    log_step "CreateContainerError detected on $COMPONENT_DEPLOY."
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

if ! echo "$status" | grep -q "CreateContainerError\|CreateContainerConfigError"; then
  log_step "WARN: CreateContainerError not detected within timeout. Check manually."
fi

log_step "ServiceAccount deleted — mlflow operator cannot start."
log_step "Run MCP tools to validate, then run serviceaccount-deleted-teardown.sh to restore."
