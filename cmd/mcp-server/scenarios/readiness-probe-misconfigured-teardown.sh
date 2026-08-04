#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
COMPONENT_DEPLOY="feast-operator-controller-manager"

log_step "=== Scenario 25: Readiness Probe Misconfigured — Teardown ==="

# Ensure operator is always restored even if rollback fails
trap 'oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=3 2>/dev/null || true' EXIT

# Rollback the deployment to restore original probe
log_step "Rolling back $COMPONENT_DEPLOY..."
oc rollout undo deployment/"$COMPONENT_DEPLOY" -n "$APPS_NS"

# Scale operator back up
log_step "Scaling $OPERATOR_DEPLOY back to 3 replicas..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=3

trap - EXIT

wait_for_deployment_ready "$OPERATOR_NS" "$OPERATOR_DEPLOY" "180s"
wait_for_deployment_ready "$APPS_NS" "$COMPONENT_DEPLOY" "180s"

log_step "Probe restored, feast operator and ODH operator running. Scenario 25 teardown complete."
