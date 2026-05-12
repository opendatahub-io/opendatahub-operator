#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"

log_step "=== Scenario 04: Operator Crash — Teardown ==="

# Scale operator back up
log_step "Scaling $OPERATOR_DEPLOY back to 3 replicas..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=3

wait_for_deployment_ready "$OPERATOR_NS" "$OPERATOR_DEPLOY" "180s"

log_step "Operator restored. Scenario 04 teardown complete."
