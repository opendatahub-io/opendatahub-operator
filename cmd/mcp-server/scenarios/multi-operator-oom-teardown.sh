#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"

DEPLOYMENTS=("feast-operator-controller-manager" "spark-operator-controller" "trustyai-service-operator-controller-manager")

log_step "=== Scenario 21: Multi-Operator OOM — Teardown ==="

# Ensure operator is always restored even if rollback fails
trap 'oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=3 2>/dev/null || true' EXIT

# Scale operator back up — it will reconcile the correct deployment specs
log_step "Scaling $OPERATOR_DEPLOY back to 3 replicas..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=3

trap - EXIT

wait_for_deployment_ready "$OPERATOR_NS" "$OPERATOR_DEPLOY" "180s"

for deploy in "${DEPLOYMENTS[@]}"; do
  log_step "Deleting $deploy so the operator can recreate it..."
  oc delete deployment "$deploy" -n "$APPS_NS" --ignore-not-found
  wait_for_deployment_ready "$APPS_NS" "$deploy" "180s"
done

log_step "All operators restored. Scenario 21 teardown complete."
