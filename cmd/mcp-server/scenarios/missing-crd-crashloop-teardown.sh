#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
COMPONENT_DEPLOY="odh-model-controller"

log_step "=== Scenario 23: Missing CRD Crashloop — Teardown ==="

# Ensure operator is always restored even if restore fails
trap 'oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=3 2>/dev/null || true' EXIT

# Scale operator back up — it will recreate the CRD via reconciliation
log_step "Scaling $OPERATOR_DEPLOY back to 3 replicas..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=3

wait_for_deployment_ready "$OPERATOR_NS" "$OPERATOR_DEPLOY" "180s"

# Wait for operator to recreate the CRD
log_step "Waiting for operator to recreate the CRD..."
sleep 30

# Restart odh-model-controller so it picks up the restored CRD
log_step "Restarting $COMPONENT_DEPLOY..."
oc rollout restart deployment "$COMPONENT_DEPLOY" -n "$APPS_NS"

trap - EXIT

wait_for_deployment_ready "$APPS_NS" "$COMPONENT_DEPLOY" "180s"

log_step "CRD restored, odh-model-controller and operator running. Scenario 23 teardown complete."
