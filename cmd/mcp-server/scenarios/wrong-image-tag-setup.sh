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
BAD_IMAGE="quay.io/opendatahub/odh-dashboard:v999-does-not-exist"

log_step "=== Scenario 17: Wrong Image Tag — Setup ==="

# Scale down the ODH operator so it doesn't revert our image change
log_step "Scaling down ODH operator to prevent reconciliation..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=0
sleep 5

# Backup original deployment
backup_resource deployment "$DEPLOYMENT" "$APPS_NS" "$BACKUP_DIR/17-${DEPLOYMENT}.json"

# Patch to valid registry but nonexistent tag
log_step "Patching $DEPLOYMENT to use image: $BAD_IMAGE"
oc set image "deployment/$DEPLOYMENT" -n "$APPS_NS" "$CONTAINER=$BAD_IMAGE"

# Wait for ImagePullBackOff
log_step "Waiting for pod to enter ImagePullBackOff..."
elapsed=0
while (( elapsed < 120 )); do
  waiting_reason=$(oc get pods -n "$APPS_NS" -l app=odh-dashboard \
    -o jsonpath='{range .items[*]}{.status.containerStatuses[?(@.name=="'"$CONTAINER"'")].state.waiting.reason}{"\n"}{end}' 2>/dev/null || echo "")
  if echo "$waiting_reason" | grep -q "ImagePullBackOff\|ErrImagePull"; then
    log_step "Pod is in ImagePullBackOff/ErrImagePull state."
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

if ! echo "$waiting_reason" | grep -q "ImagePullBackOff\|ErrImagePull"; then
  log_step "WARN: Pod did not enter ImagePullBackOff within timeout (current: $waiting_reason)"
fi

log_step "Wrong image tag failure injected."
log_step "Run MCP tools to validate, then run wrong-image-tag-teardown.sh to restore."
