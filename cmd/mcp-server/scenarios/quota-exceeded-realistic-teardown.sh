#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
QUOTA_NAME="scenario-tight-quota"

log_step "=== Scenario 14: Quota Exceeded Realistic — Teardown ==="

# Delete the restrictive quota
log_step "Deleting ResourceQuota $QUOTA_NAME from $APPS_NS..."
oc delete resourcequota "$QUOTA_NAME" -n "$APPS_NS" --ignore-not-found

# Scale operator back up
log_step "Scaling $OPERATOR_DEPLOY back to 3 replicas..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=3

wait_for_deployment_ready "$OPERATOR_NS" "$OPERATOR_DEPLOY" "180s"

log_step "Quota removed and operator restored. Scenario 14 teardown complete."
