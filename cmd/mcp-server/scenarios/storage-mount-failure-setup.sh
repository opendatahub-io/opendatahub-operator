#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
DEPLOYMENT="odh-dashboard"
FAKE_SECRET="scenario-nonexistent-secret"

log_step "=== Scenario 12: Storage Mount Failure — Setup ==="

# Scale down the ODH operator so it doesn't revert our change
log_step "Scaling down ODH operator to prevent reconciliation..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=0
sleep 5

# Backup original deployment
backup_resource deployment "$DEPLOYMENT" "$APPS_NS" "$BACKUP_DIR/12-${DEPLOYMENT}.json"

# Patch deployment to add a volume referencing a nonexistent secret.
# Secret volumes cause FailedMount events (unlike PVC volumes which cause FailedScheduling).
log_step "Patching $DEPLOYMENT to mount nonexistent secret '$FAKE_SECRET'..."
oc patch deployment "$DEPLOYMENT" -n "$APPS_NS" --type=json -p '[
  {"op":"add","path":"/spec/template/spec/volumes/-","value":{"name":"fake-vol","secret":{"secretName":"'"$FAKE_SECRET"'"}}},
  {"op":"add","path":"/spec/template/spec/containers/0/volumeMounts/-","value":{"name":"fake-vol","mountPath":"/mnt/fake"}}
]'

# Wait for FailedMount events
log_step "Waiting for FailedMount events..."
elapsed=0
while (( elapsed < 120 )); do
  event_count=$(oc get events -n "$APPS_NS" --field-selector reason=FailedMount --no-headers 2>/dev/null | wc -l || echo "0")
  event_count=$((event_count))
  if (( event_count > 0 )); then
    log_step "FailedMount events detected ($event_count found)."
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

if (( event_count == 0 )); then
  log_step "WARN: No FailedMount events detected within timeout."
fi

log_step "Storage mount failure injected."
log_step "Run MCP tools to validate, then run storage-mount-failure-teardown.sh to restore."
