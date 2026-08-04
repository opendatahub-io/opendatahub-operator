#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
CRD_NAME="inferenceservices.serving.kserve.io"
COMPONENT_DEPLOY="odh-model-controller"

log_step "=== Scenario 23: Missing CRD Crashloop (RHOAIENG-42017) — Setup ==="

# Scale down the ODH operator so it doesn't recreate the CRD
log_step "Scaling down ODH operator to prevent reconciliation..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=0
while [[ "$(oc get deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" -o jsonpath='{.status.readyReplicas}' 2>/dev/null)" != "0" ]] \
  && [[ -n "$(oc get deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" -o jsonpath='{.status.readyReplicas}' 2>/dev/null)" ]]; do
  sleep 2
done

# Backup the CRD before deletion
log_step "Backing up CRD $CRD_NAME..."
oc get crd "$CRD_NAME" -o json > "$BACKUP_DIR/23-${CRD_NAME}.json"

# Delete the CRD
log_step "Deleting CRD $CRD_NAME..."
oc delete crd "$CRD_NAME"

# Restart odh-model-controller so it crashes on the missing CRD
log_step "Restarting $COMPONENT_DEPLOY to trigger CRD watch failure..."
oc rollout restart deployment "$COMPONENT_DEPLOY" -n "$APPS_NS"

# Wait for CrashLoopBackOff
log_step "Waiting for $COMPONENT_DEPLOY to enter CrashLoopBackOff..."
elapsed=0
while (( elapsed < 120 )); do
  status=$(oc get pods -n "$APPS_NS" -o json | jq -r --arg deploy "$COMPONENT_DEPLOY" \
    '.items[] | select(.metadata.name | startswith($deploy)) | .status.containerStatuses[]? | "\(.state.waiting.reason // "") \(.lastState.terminated.reason // "")"' 2>/dev/null)
  if echo "$status" | grep -q "CrashLoopBackOff\|Error"; then
    log_step "CrashLoopBackOff detected on $COMPONENT_DEPLOY."
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

if ! echo "$status" | grep -q "CrashLoopBackOff\|Error"; then
  log_step "WARN: $COMPONENT_DEPLOY not in CrashLoopBackOff within timeout. Check manually."
fi

log_step "CRD deleted — odh-model-controller crashing on missing InferenceService kind."
log_step "Run MCP tools to validate, then run missing-crd-crashloop-teardown.sh to restore."
