#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
CRB_NAME="model-registry-operator-manager-rolebinding"
COMPONENT_DEPLOY="model-registry-operator-controller-manager"

log_step "=== Scenario 22: RBAC Blocks Reconciliation (RHOAIENG-54987) — Setup ==="

# Scale down the ODH operator so it doesn't recreate the ClusterRoleBinding
log_step "Scaling down ODH operator to prevent reconciliation..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=0
while [[ "$(oc get deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" -o jsonpath='{.status.readyReplicas}' 2>/dev/null)" != "0" ]] \
  && [[ -n "$(oc get deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" -o jsonpath='{.status.readyReplicas}' 2>/dev/null)" ]]; do
  sleep 2
done

# Backup the ClusterRoleBinding before deletion
log_step "Backing up ClusterRoleBinding $CRB_NAME..."
backup_resource clusterrolebinding "$CRB_NAME" "" "$BACKUP_DIR/22-${CRB_NAME}.json"

# Delete the ClusterRoleBinding to remove all cluster-scoped permissions
log_step "Deleting ClusterRoleBinding $CRB_NAME..."
oc delete clusterrolebinding "$CRB_NAME"

# Restart the model-registry controller so it picks up the missing permissions
log_step "Restarting $COMPONENT_DEPLOY to trigger informer reconnect..."
oc rollout restart deployment "$COMPONENT_DEPLOY" -n "$APPS_NS"
oc rollout status deployment "$COMPONENT_DEPLOY" -n "$APPS_NS" --timeout=60s || true

# Wait for 403 errors to appear in logs
log_step "Waiting for 403 Forbidden errors in model-registry operator logs..."
elapsed=0
while (( elapsed < 120 )); do
  forbidden_count=$(oc logs deployment/"$COMPONENT_DEPLOY" -n "$APPS_NS" --tail=100 2>/dev/null | grep -ci "forbidden\|403" || true)
  if (( forbidden_count >= 1 )); then
    log_step "403 Forbidden errors detected in model-registry logs ($forbidden_count occurrences)."
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

if (( forbidden_count < 1 )); then
  log_step "WARN: No 403 errors detected within timeout. Check manually: oc logs deployment/$COMPONENT_DEPLOY -n $APPS_NS"
fi

log_step "RBAC removed — model-registry operator running without cluster-scoped permissions."
log_step "Run MCP tools to validate, then run rbac-blocks-reconciliation-teardown.sh to restore."
