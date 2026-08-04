#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
COMPONENT_DEPLOY="feast-operator-controller-manager"
CONTAINER="manager"

log_step "=== Scenario 25: Readiness Probe Misconfigured (troubleshooting.md) — Setup ==="

# Scale down the ODH operator so it doesn't revert our probe changes
log_step "Scaling down ODH operator to prevent reconciliation..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=0
sleep 5

# Backup original deployment
log_step "Backing up $COMPONENT_DEPLOY..."
backup_resource deployment "$COMPONENT_DEPLOY" "$APPS_NS" "$BACKUP_DIR/25-${COMPONENT_DEPLOY}.json"

# Find container index
CONTAINER_INDEX=$(oc get deployment "$COMPONENT_DEPLOY" -n "$APPS_NS" -o json | \
  jq --arg name "$CONTAINER" '.spec.template.spec.containers | to_entries[] | select(.value.name == $name) | .key')

if [[ -z "$CONTAINER_INDEX" ]]; then
  log_step "ERROR: Container '$CONTAINER' not found in deployment '$COMPONENT_DEPLOY'"
  exit 1
fi

# Patch readiness probe to wrong port
log_step "Patching readiness probe on $COMPONENT_DEPLOY to port 9999..."
oc patch deployment "$COMPONENT_DEPLOY" -n "$APPS_NS" --type=json \
  -p '[{"op":"replace","path":"/spec/template/spec/containers/'"$CONTAINER_INDEX"'/readinessProbe/httpGet/port","value":9999}]'

# Wait for pod to be Running but not Ready (0/1)
log_step "Waiting for pod to be Running but not Ready..."
elapsed=0
while (( elapsed < 120 )); do
  ready=$(oc get pods -n "$APPS_NS" -o json | jq -r --arg deploy "$COMPONENT_DEPLOY" \
    '[.items[] | select(.metadata.name | startswith($deploy)) | select(.status.phase == "Running") | .status.containerStatuses[]? | select(.ready == false)] | length' 2>/dev/null)
  if (( ready > 0 )); then
    log_step "Pod Running but not Ready detected (0/1)."
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

if (( ready == 0 )); then
  log_step "WARN: Pod not in expected state within timeout. Check manually."
fi

log_step "Readiness probe misconfigured — pod Running but not Ready."
log_step "Run MCP tools to validate, then run readiness-probe-misconfigured-teardown.sh to restore."
