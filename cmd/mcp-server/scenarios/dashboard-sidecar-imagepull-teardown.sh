#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
DEPLOYMENT="odh-dashboard"

log_step "=== Scenario 20: Dashboard Sidecar ImagePull — Teardown ==="

# Ensure operator is always restored even if rollback fails
trap 'oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=3 2>/dev/null || true' EXIT

# Rollback the deployment to restore original images
log_step "Rolling back $DEPLOYMENT deployment..."
oc rollout undo deployment/"$DEPLOYMENT" -n "$APPS_NS"

# Scale operator back up
log_step "Scaling $OPERATOR_DEPLOY back to 3 replicas..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=3

trap - EXIT

wait_for_deployment_ready "$OPERATOR_NS" "$OPERATOR_DEPLOY" "180s"
wait_for_deployment_ready "$APPS_NS" "$DEPLOYMENT" "180s"

log_step "Operator and dashboard restored. Scenario 20 teardown complete."
