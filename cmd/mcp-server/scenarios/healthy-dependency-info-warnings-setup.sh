#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

log_step "=== Scenario 29: Healthy Dependency Info Warnings — Setup ==="

# This scenario uses the cluster's natural state — optional dependencies (Kuadrant, JobSet)
# are not installed, producing Info-severity warnings in DSC conditions.
# No injection needed if these dependencies are already absent.

# Verify the Info-severity warnings exist
KSERVE_LLM_STATUS=$(oc get dsc -o jsonpath='{.items[0].status.conditions[?(@.type=="KserveLLMInferenceServiceDependencies")].status}' 2>/dev/null || echo "")
KSERVE_LLM_SEVERITY=$(oc get dsc -o jsonpath='{.items[0].status.conditions[?(@.type=="KserveLLMInferenceServiceDependencies")].severity}' 2>/dev/null || echo "")
KSERVE_WIDEEP_STATUS=$(oc get dsc -o jsonpath='{.items[0].status.conditions[?(@.type=="KserveLLMInferenceServiceWideEPDependencies")].status}' 2>/dev/null || echo "")
KSERVE_WIDEEP_SEVERITY=$(oc get dsc -o jsonpath='{.items[0].status.conditions[?(@.type=="KserveLLMInferenceServiceWideEPDependencies")].severity}' 2>/dev/null || echo "")
TRAINER_STATUS=$(oc get dsc -o jsonpath='{.items[0].status.conditions[?(@.type=="TrainerReady")].status}' 2>/dev/null || echo "")
TRAINER_MESSAGE=$(oc get dsc -o jsonpath='{.items[0].status.conditions[?(@.type=="TrainerReady")].message}' 2>/dev/null || echo "")

log_step "KserveLLMInferenceServiceDependencies: status=$KSERVE_LLM_STATUS severity=$KSERVE_LLM_SEVERITY"
log_step "KserveLLMInferenceServiceWideEPDependencies: status=$KSERVE_WIDEEP_STATUS severity=$KSERVE_WIDEEP_SEVERITY"
log_step "TrainerReady: status=$TRAINER_STATUS"

if [[ "$KSERVE_LLM_STATUS" == "False" ]] \
  && [[ "$KSERVE_LLM_SEVERITY" == "Info" ]] \
  && [[ "$KSERVE_WIDEEP_STATUS" == "False" ]] \
  && [[ "$KSERVE_WIDEEP_SEVERITY" == "Info" ]] \
  && [[ "$TRAINER_STATUS" == "False" ]] \
  && grep -qi "jobset" <<<"$TRAINER_MESSAGE"; then
  log_step "Info-severity dependency warnings present — false-positive condition active."
else
  log_step "ERROR: Cluster does not match healthy-dependency-info-warnings prerequisites."
  exit 1
fi

# Verify all component pods are healthy
APPS_NS=$(discover_apps_namespace)
UNHEALTHY_PODS=$(oc get pods -n "$APPS_NS" --field-selector='status.phase!=Running,status.phase!=Succeeded' --no-headers 2>/dev/null | wc -l)
if (( UNHEALTHY_PODS == 0 )); then
  log_step "All pods healthy — agent should report 'Platform Healthy' despite False conditions with Info severity."
else
  log_step "WARN: $UNHEALTHY_PODS unhealthy pods found — resolve before running this false-positive test."
fi

log_step "Run MCP tools to validate. Agent should not escalate Info-severity conditions as failures."
log_step "Run healthy-dependency-info-warnings-teardown.sh to clean up."
