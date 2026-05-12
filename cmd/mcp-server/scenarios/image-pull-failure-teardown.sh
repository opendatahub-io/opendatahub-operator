#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
DEPLOYMENT="odh-dashboard"

log_step "=== Scenario 02: Image Pull Failure — Teardown ==="

# Scale ODH operator back up — it will reconcile and restore the correct image
log_step "Scaling ODH operator back up..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=3

wait_for_deployment_ready "$OPERATOR_NS" "$OPERATOR_DEPLOY" "180s"

# Wait for operator to reconcile the dashboard deployment
log_step "Waiting for operator to reconcile $DEPLOYMENT..."
sleep 15

wait_for_deployment_ready "$APPS_NS" "$DEPLOYMENT" "180s"

log_step "$DEPLOYMENT restored. Scenario 02 teardown complete."
