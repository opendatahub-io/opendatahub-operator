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

log_step "=== Scenario 30: Healthy With Stale Events — Setup ==="

# Step 1: Inject a temporary failure to generate warning events
log_step "Scaling down ODH operator..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=0
sleep 5

# Patch feast-operator memory to 5Mi to generate OOMKilled events
CONTAINER_INDEX=$(oc get deployment "$COMPONENT_DEPLOY" -n "$APPS_NS" -o json | \
  jq --arg name "$CONTAINER" '.spec.template.spec.containers | to_entries[] | select(.value.name == $name) | .key')

if [[ -z "$CONTAINER_INDEX" ]]; then
  log_step "ERROR: Container '$CONTAINER' not found in deployment '$COMPONENT_DEPLOY'"
  exit 1
fi

log_step "Temporarily injecting OOM failure to generate warning events..."
oc patch deployment "$COMPONENT_DEPLOY" -n "$APPS_NS" --type=json \
  -p '[{"op":"replace","path":"/spec/template/spec/containers/'"$CONTAINER_INDEX"'/resources/limits/memory","value":"5Mi"},{"op":"replace","path":"/spec/template/spec/containers/'"$CONTAINER_INDEX"'/resources/requests/memory","value":"5Mi"}]'

# Wait for OOMKilled events to be generated
log_step "Waiting for OOMKilled events to be generated..."
elapsed=0
while (( elapsed < 120 )); do
  event_count=$(oc get events -n "$APPS_NS" --sort-by='.lastTimestamp' 2>/dev/null | grep -c "OOMKilled\|BackOff\|Unhealthy" || true)
  if (( event_count > 0 )); then
    log_step "OOM warning events detected ($event_count)."
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

if (( event_count == 0 )); then
  log_step "WARN: No OOM events observed before timeout. Continuing with partial state."
fi

# Step 2: Restore the deployment — events will persist but failure is resolved
log_step "Restoring feast-operator to healthy state..."
oc rollout undo deployment/"$COMPONENT_DEPLOY" -n "$APPS_NS"

# Scale operator back up
log_step "Scaling ODH operator back to 3 replicas..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=3

wait_for_deployment_ready "$OPERATOR_NS" "$OPERATOR_DEPLOY" "180s"
wait_for_deployment_ready "$APPS_NS" "$COMPONENT_DEPLOY" "180s"

# Verify cluster is healthy but events remain
STALE_EVENTS=$(oc get events -n "$APPS_NS" --sort-by='.lastTimestamp' | grep -c "OOMKilled\|BackOff\|Unhealthy\|FailedMount" || true)
log_step "Cluster restored to healthy state. $STALE_EVENTS stale warning events remain."

UNHEALTHY_PODS=$(oc get pods -n "$APPS_NS" --field-selector='status.phase!=Running,status.phase!=Succeeded' --no-headers 2>/dev/null | wc -l)
if (( UNHEALTHY_PODS == 0 )); then
  log_step "All pods healthy — false-positive condition active (stale events + healthy pods)."
else
  log_step "WARN: $UNHEALTHY_PODS unhealthy pods found — wait for full recovery before testing."
fi

log_step "Run MCP tools NOW. Agent should report 'Platform Healthy' despite warning events."
log_step "Run healthy-stale-events-teardown.sh to clean up."
