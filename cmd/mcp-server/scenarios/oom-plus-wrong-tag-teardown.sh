#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
TRUSTYAI_DEPLOY="trustyai-service-operator-controller-manager"
DASHBOARD_DEPLOY="odh-dashboard"

log_step "=== Scenario 26: OOM Plus Wrong Tag — Teardown ==="

# Ensure operator is always restored even if rollback fails
trap 'oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=3 2>/dev/null || true' EXIT

# Rollback both deployments
log_step "Rolling back $TRUSTYAI_DEPLOY..."
oc rollout undo deployment/"$TRUSTYAI_DEPLOY" -n "$APPS_NS"
log_step "Rolling back $DASHBOARD_DEPLOY..."
oc rollout undo deployment/"$DASHBOARD_DEPLOY" -n "$APPS_NS"

# Scale operator back up
log_step "Scaling $OPERATOR_DEPLOY back to 3 replicas..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=3

trap - EXIT

wait_for_deployment_ready "$OPERATOR_NS" "$OPERATOR_DEPLOY" "180s"
wait_for_deployment_ready "$APPS_NS" "$TRUSTYAI_DEPLOY" "180s"
wait_for_deployment_ready "$APPS_NS" "$DASHBOARD_DEPLOY" "180s"

log_step "Both components and operator restored. Scenario 26 teardown complete."
