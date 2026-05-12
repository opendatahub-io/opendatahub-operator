#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"

log_step "=== Scenario 03: Resource Exhaustion — Setup ==="

# Save original replica count before scaling
ORIG_REPLICAS=$(oc get deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" -o jsonpath='{.spec.replicas}' 2>/dev/null || echo "3")
STATE_DIR="${XDG_RUNTIME_DIR:-/tmp}/odh-mcp-scenarios-${UID}"
mkdir -p "$STATE_DIR"
chmod 700 "$STATE_DIR"
printf '%s\n' "$ORIG_REPLICAS" > "$STATE_DIR/odh-resource-exhaustion-replicas"

# Scale down the ODH operator so it doesn't interfere
log_step "Scaling down ODH operator to prevent reconciliation..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=0

trap 'oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas="${ORIG_REPLICAS}" 2>/dev/null || true' EXIT

sleep 5

# Apply restrictive quota
log_step "Applying restrictive ResourceQuota to $APPS_NS..."
oc apply -f "$SCRIPT_DIR/resource-exhaustion-quota.yaml" -n "$APPS_NS"

# Trigger a pod restart so new pods hit the quota
log_step "Deleting dashboard pods to trigger reschedule against quota..."
oc delete pod -l app=odh-dashboard -n "$APPS_NS" --ignore-not-found

# Wait for quota-exceeded events
log_step "Waiting for FailedCreate events..."
elapsed=0
events=0
while (( elapsed < 90 )); do
  events=$(oc get events -n "$APPS_NS" --field-selector reason=FailedCreate --no-headers 2>/dev/null | wc -l || echo "0")
  events=$((events))
  if (( events > 0 )); then
    log_step "FailedCreate events detected ($events found)."
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

if (( events == 0 )); then
  log_step "WARN: No FailedCreate events detected within timeout."
fi

trap - EXIT

log_step "Resource exhaustion injected."
log_step "Run MCP tools to validate, then run resource-exhaustion-teardown.sh to restore."
