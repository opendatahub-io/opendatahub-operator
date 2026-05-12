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
FAKE_IMAGE="quay.io/nonexistent/image:v999"

log_step "=== Scenario 02: Image Pull Failure — Setup ==="

# Scale down the ODH operator so it doesn't revert our image change
log_step "Scaling down ODH operator to prevent reconciliation..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=0
sleep 5

# Backup original deployment
backup_resource deployment "$DEPLOYMENT" "$APPS_NS" "$BACKUP_DIR/02-${DEPLOYMENT}.json"

# Patch to nonexistent image
log_step "Patching $DEPLOYMENT to use image: $FAKE_IMAGE"
oc set image "deployment/$DEPLOYMENT" -n "$APPS_NS" "$CONTAINER=$FAKE_IMAGE"

# Wait for ImagePullBackOff
log_step "Waiting for pod to enter ImagePullBackOff..."
elapsed=0
while (( elapsed < 120 )); do
  waiting_reason=$(oc get pods -n "$APPS_NS" -l "app=$DEPLOYMENT" \
    -o jsonpath='{.items[0].status.containerStatuses[?(@.name=="'"$CONTAINER"'")].state.waiting.reason}' 2>/dev/null || echo "")
  if [[ "$waiting_reason" == "ImagePullBackOff" || "$waiting_reason" == "ErrImagePull" ]]; then
    log_step "Pod is in $waiting_reason state."
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

if [[ "$waiting_reason" != "ImagePullBackOff" && "$waiting_reason" != "ErrImagePull" ]]; then
  log_step "WARN: Pod did not enter ImagePullBackOff within timeout (current: $waiting_reason)"
fi

log_step "Image pull failure injected."
log_step "Run MCP tools to validate, then run image-pull-failure-teardown.sh to restore."
