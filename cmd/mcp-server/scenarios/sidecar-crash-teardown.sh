#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
DEPLOYMENT="odh-dashboard"

log_step "=== Scenario 19: Sidecar Container Crash — Teardown ==="

# Rollback the deployment to restore original sidecar command
log_step "Rolling back $DEPLOYMENT deployment..."
oc rollout undo deployment/"$DEPLOYMENT" -n "$APPS_NS"

# Scale operator back up
log_step "Scaling $OPERATOR_DEPLOY back to 3 replicas..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=3

wait_for_deployment_ready "$OPERATOR_NS" "$OPERATOR_DEPLOY" "180s"
wait_for_deployment_ready "$APPS_NS" "$DEPLOYMENT" "180s"

log_step "Operator and dashboard restored. Scenario 19 teardown complete."
