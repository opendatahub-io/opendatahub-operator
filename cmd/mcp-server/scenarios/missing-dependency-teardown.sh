#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

CERT_OPERATOR_NS="cert-manager-operator"
CERT_OPERATOR_DEPLOY="cert-manager-operator-controller-manager"

log_step "=== Scenario 01: Missing Dependency — Teardown ==="

# Restore cert-manager operator
log_step "Scaling $CERT_OPERATOR_DEPLOY back up..."
oc scale deployment "$CERT_OPERATOR_DEPLOY" -n "$CERT_OPERATOR_NS" --replicas=1

# Wait for readiness
wait_for_deployment_ready "$CERT_OPERATOR_NS" "$CERT_OPERATOR_DEPLOY" "180s"

log_step "cert-manager operator restored. Scenario 01 teardown complete."
