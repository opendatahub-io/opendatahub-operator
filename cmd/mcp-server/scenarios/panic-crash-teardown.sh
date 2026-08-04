#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
DEPLOYMENT="feast-operator-controller-manager"

log_step "=== Scenario 18: Panic Crash — Teardown ==="

# Delete the deployment so the operator recreates it with the correct spec
log_step "Deleting $DEPLOYMENT so operator can recreate it..."
oc delete deployment "$DEPLOYMENT" -n "$APPS_NS" --ignore-not-found

# Scale operator back up
log_step "Scaling $OPERATOR_DEPLOY back to 3 replicas..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=3

wait_for_deployment_ready "$OPERATOR_NS" "$OPERATOR_DEPLOY" "180s"

# Wait for operator to recreate the deployment
log_step "Waiting for operator to recreate $DEPLOYMENT..."
elapsed=0
while (( elapsed < 180 )); do
  ready=$(oc get deployment "$DEPLOYMENT" -n "$APPS_NS" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
  if (( ready > 0 )); then
    log_step "$DEPLOYMENT is ready ($ready replicas)."
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

if (( ready == 0 )); then
  log_step "WARN: $DEPLOYMENT did not become ready within timeout."
fi

log_step "Operator and component restored. Scenario 18 teardown complete."
