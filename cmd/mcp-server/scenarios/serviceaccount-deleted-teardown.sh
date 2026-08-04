#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
COMPONENT_DEPLOY="mlflow-operator-controller-manager"
SA_NAME="mlflow-operator-controller-manager"

log_step "=== Scenario 24: ServiceAccount Deleted — Teardown ==="

# Ensure operator is always restored even if SA restore fails
trap 'oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=3 2>/dev/null || true' EXIT

# Recreate the ServiceAccount if missing
log_step "Restoring ServiceAccount $SA_NAME..."
if ! oc get serviceaccount "$SA_NAME" -n "$APPS_NS" >/dev/null 2>&1; then
  oc create serviceaccount "$SA_NAME" -n "$APPS_NS"
fi

# Restart the component to pick up the restored SA
log_step "Restarting $COMPONENT_DEPLOY..."
oc rollout restart deployment "$COMPONENT_DEPLOY" -n "$APPS_NS"

# Scale operator back up
log_step "Scaling $OPERATOR_DEPLOY back to 3 replicas..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=3

trap - EXIT

wait_for_deployment_ready "$OPERATOR_NS" "$OPERATOR_DEPLOY" "180s"
wait_for_deployment_ready "$APPS_NS" "$COMPONENT_DEPLOY" "180s"

log_step "ServiceAccount restored, mlflow operator and ODH operator running. Scenario 24 teardown complete."
