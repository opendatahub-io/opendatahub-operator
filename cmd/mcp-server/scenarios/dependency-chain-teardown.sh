#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

JOBSET_NS="jobset-system"
JOBSET_DEPLOY="jobset-controller-manager"

log_step "=== Scenario: Dependency Chain (jobset → Trainer) — Teardown ==="

log_step "Scaling $JOBSET_DEPLOY back to 1 in $JOBSET_NS..."
oc scale deployment "$JOBSET_DEPLOY" -n "$JOBSET_NS" --replicas=1

log_step "Waiting for $JOBSET_DEPLOY to become ready..."
wait_for_deployment_ready "$JOBSET_NS" "$JOBSET_DEPLOY" "180s" || true

log_step "Waiting for DSC to reflect recovery..."
sleep 45

JOBSET_AVAIL=$(oc get jobsetoperator cluster -o jsonpath='{.status.conditions[?(@.type=="Available")].status}' 2>/dev/null || echo "unknown")
TRAINER_STATUS=$(oc get dsc -A -o jsonpath='{.items[0].status.conditions[?(@.type=="TrainerReady")].status}' 2>/dev/null || echo "unknown")
log_step "JobSetOperator Available: $JOBSET_AVAIL (expected: True)"
log_step "TrainerReady: $TRAINER_STATUS (expected: True)"

log_step "Dependency chain scenario teardown complete."
