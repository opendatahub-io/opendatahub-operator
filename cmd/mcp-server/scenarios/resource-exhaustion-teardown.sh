#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"

log_step "=== Scenario 03: Resource Exhaustion — Teardown ==="

# Remove the restrictive quota
log_step "Deleting ResourceQuota scenario-low-quota..."
oc delete resourcequota scenario-low-quota -n "$APPS_NS" --ignore-not-found

# Restore operator to original replica count
STATE_DIR="${XDG_RUNTIME_DIR:-/tmp}/odh-mcp-scenarios-${UID}"
ORIG_REPLICAS=3
if [[ -f "$STATE_DIR/odh-resource-exhaustion-replicas" ]]; then
  ORIG_REPLICAS=$(cat "$STATE_DIR/odh-resource-exhaustion-replicas")
  if ! [[ "$ORIG_REPLICAS" =~ ^[0-9]+$ ]]; then
    log_step "WARN: Invalid replica count in state file, defaulting to 3."
    ORIG_REPLICAS=3
  fi
  rm -f "$STATE_DIR/odh-resource-exhaustion-replicas"
fi

log_step "Scaling ODH operator back up to $ORIG_REPLICAS replicas..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas="$ORIG_REPLICAS"

wait_for_deployment_ready "$OPERATOR_NS" "$OPERATOR_DEPLOY" "180s"

# Wait for operator to reconcile
log_step "Waiting for operator to reconcile..."
sleep 20

wait_for_deployment_ready "$APPS_NS" "odh-dashboard" "200s"

log_step "Quota removed, cluster restored. Scenario 03 teardown complete."
