#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

JOBSET_NS="jobset-system"
JOBSET_DEPLOY="jobset-controller-manager"

log_step "=== Scenario: Dependency Chain (jobset → Trainer) — Setup ==="

log_step "Scaling $JOBSET_DEPLOY to 0 in $JOBSET_NS (prevents CR reconciliation)..."
oc scale deployment "$JOBSET_DEPLOY" -n "$JOBSET_NS" --replicas=0

log_step "Waiting for $JOBSET_DEPLOY pod to terminate..."
elapsed=0
while (( elapsed < 60 )); do
  pods=$(oc get pods -n "$JOBSET_NS" \
    --field-selector=status.phase=Running --no-headers 2>/dev/null | wc -l)
  if (( pods == 0 )); then
    log_step "$JOBSET_DEPLOY pod terminated."
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

if (( pods > 0 )); then
  log_step "ERROR: Timed out waiting for $JOBSET_DEPLOY pod to terminate (${pods} pods still running). CR patch would be reconciled back immediately."
  exit 1
fi

log_step "Patching JobSetOperator CR 'cluster' to Available=False..."
TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
oc patch jobsetoperator cluster --type=merge --subresource=status -p "{
  \"status\": {
    \"conditions\": [
      {\"type\": \"Available\", \"status\": \"False\", \"reason\": \"DeploymentUnavailable\", \"message\": \"Operand Deployment is not available\", \"lastTransitionTime\": \"$TIMESTAMP\"},
      {\"type\": \"Degraded\", \"status\": \"True\", \"reason\": \"DeploymentUnavailable\", \"message\": \"Operand Deployment is not available\", \"lastTransitionTime\": \"$TIMESTAMP\"}
    ]
  }
}"

log_step "Waiting for DSC to reflect failure (TrainerReady=False)..."
elapsed=0
TRAINER_STATUS="unknown"
TRAINER_REASON="unknown"
while (( elapsed < 150 )); do
  TRAINER_STATUS=$(oc get dsc -A -o jsonpath='{.items[0].status.conditions[?(@.type=="TrainerReady")].status}' 2>/dev/null || echo "unknown")
  TRAINER_REASON=$(oc get dsc -A -o jsonpath='{.items[0].status.conditions[?(@.type=="TrainerReady")].reason}' 2>/dev/null || echo "unknown")
  if [[ "$TRAINER_STATUS" == "False" ]]; then
    log_step "TrainerReady: $TRAINER_STATUS ($TRAINER_REASON)"
    break
  fi
  sleep 10
  (( elapsed += 10 ))
done

if [[ "$TRAINER_STATUS" != "False" ]]; then
  log_step "ERROR: TrainerReady did not become False within timeout (current: $TRAINER_STATUS/$TRAINER_REASON). Scenario is invalid."
  exit 1
fi

log_step "Dependency chain scenario setup complete."
