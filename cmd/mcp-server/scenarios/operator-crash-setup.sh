#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"

log_step "=== Scenario 04: Operator Crash — Setup ==="

# Backup operator deployment
backup_resource deployment "$OPERATOR_DEPLOY" "$OPERATOR_NS" "$BACKUP_DIR/04-${OPERATOR_DEPLOY}.json"

# Scale operator to 0 replicas to simulate operator being down
log_step "Scaling $OPERATOR_DEPLOY to 0 replicas..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=0

# Wait for operator pods to terminate
log_step "Waiting for operator pods to terminate..."
elapsed=0
while (( elapsed < 60 )); do
  pod_count=$(oc get pods -n "$OPERATOR_NS" -l control-plane=controller-manager \
    --field-selector=status.phase=Running --no-headers 2>/dev/null | wc -l)
  if (( pod_count == 0 )); then
    log_step "All operator pods terminated."
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

log_step "Operator is down. Failure injected."
log_step "Run MCP tools to validate, then run operator-crash-teardown.sh to restore."
