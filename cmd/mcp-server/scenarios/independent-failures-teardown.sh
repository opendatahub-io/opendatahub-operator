#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)

MODEL_REGISTRY_DEPLOY="model-registry-operator-controller-manager"
PIPELINES_DEPLOY="data-science-pipelines-operator-controller-manager"

log_step "=== Scenario: Independent Failures — Teardown ==="

log_step "Scaling $MODEL_REGISTRY_DEPLOY back to 1 replica..."
oc scale deployment "$MODEL_REGISTRY_DEPLOY" -n "$APPS_NS" --replicas=1

log_step "Scaling $PIPELINES_DEPLOY back to 1 replica..."
oc scale deployment "$PIPELINES_DEPLOY" -n "$APPS_NS" --replicas=1

log_step "Waiting for deployments to become ready..."
wait_for_deployment_ready "$APPS_NS" "$MODEL_REGISTRY_DEPLOY" "180s" || true
wait_for_deployment_ready "$APPS_NS" "$PIPELINES_DEPLOY" "180s" || true

log_step "Waiting for DSC to reflect recovery..."
sleep 30

MR_STATUS=$(oc get dsc -A -o jsonpath='{.items[0].status.conditions[?(@.type=="ModelRegistryReady")].status}' 2>/dev/null || echo "unknown")
DSP_STATUS=$(oc get dsc -A -o jsonpath='{.items[0].status.conditions[?(@.type=="AIPipelinesReady")].status}' 2>/dev/null || echo "unknown")
log_step "ModelRegistryReady: $MR_STATUS (expected: True)"
log_step "AIPipelinesReady: $DSP_STATUS (expected: True)"

log_step "Independent failures scenario teardown complete."
