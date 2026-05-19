#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)

MODEL_REGISTRY_DEPLOY="model-registry-operator-controller-manager"
PIPELINES_DEPLOY="data-science-pipelines-operator-controller-manager"

log_step "=== Scenario: Independent Failures — Setup ==="

log_step "Scaling $MODEL_REGISTRY_DEPLOY to 0 replicas..."
oc scale deployment "$MODEL_REGISTRY_DEPLOY" -n "$APPS_NS" --replicas=0

log_step "Scaling $PIPELINES_DEPLOY to 0 replicas..."
oc scale deployment "$PIPELINES_DEPLOY" -n "$APPS_NS" --replicas=0

log_step "Waiting for pods to terminate..."
elapsed=0
while (( elapsed < 60 )); do
  mr_pods=$(oc get pods -n "$APPS_NS" -l app.kubernetes.io/name=model-registry-operator \
    --field-selector=status.phase=Running --no-headers 2>/dev/null | wc -l)
  dsp_pods=$(oc get pods -n "$APPS_NS" -l app.kubernetes.io/name=data-science-pipelines-operator \
    --field-selector=status.phase=Running --no-headers 2>/dev/null | wc -l)
  if (( mr_pods == 0 && dsp_pods == 0 )); then
    log_step "All target pods terminated."
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

log_step "Waiting for DSC to reflect failures..."
sleep 30

log_step "Verifying failure state..."
MR_STATUS=$(oc get dsc -A -o jsonpath='{.items[0].status.conditions[?(@.type=="ModelRegistryReady")].status}' 2>/dev/null || echo "unknown")
DSP_STATUS=$(oc get dsc -A -o jsonpath='{.items[0].status.conditions[?(@.type=="AIPipelinesReady")].status}' 2>/dev/null || echo "unknown")
log_step "ModelRegistryReady: $MR_STATUS (expected: False)"
log_step "AIPipelinesReady: $DSP_STATUS (expected: False)"

log_step "Independent failures scenario setup complete."
