#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"

DEPLOYMENTS=("feast-operator-controller-manager" "spark-operator-controller" "trustyai-service-operator-controller-manager")
CONTAINERS=("manager" "controller" "manager")

log_step "=== Scenario 21: Multi-Operator OOM (RHOAIENG-52932 + 53745) — Setup ==="

# Scale down the ODH operator so it doesn't revert our memory changes
log_step "Scaling down ODH operator to prevent reconciliation..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=0
sleep 5

# Backup and patch each operator
for i in "${!DEPLOYMENTS[@]}"; do
  deploy="${DEPLOYMENTS[$i]}"
  container="${CONTAINERS[$i]}"

  log_step "Backing up $deploy..."
  backup_resource deployment "$deploy" "$APPS_NS" "$BACKUP_DIR/21-${deploy}.json"

  CONTAINER_INDEX=$(oc get deployment "$deploy" -n "$APPS_NS" -o json | \
    jq --arg name "$container" '.spec.template.spec.containers | to_entries[] | select(.value.name == $name) | .key')

  if [[ -z "$CONTAINER_INDEX" ]]; then
    log_step "ERROR: Container '$container' not found in deployment '$deploy'"
    exit 1
  fi

  log_step "Patching $deploy container $container (index $CONTAINER_INDEX) memory to 5Mi..."
  oc patch deployment "$deploy" -n "$APPS_NS" --type=json \
    -p '[{"op":"replace","path":"/spec/template/spec/containers/'"$CONTAINER_INDEX"'/resources/limits/memory","value":"5Mi"},{"op":"replace","path":"/spec/template/spec/containers/'"$CONTAINER_INDEX"'/resources/requests/memory","value":"5Mi"}]'
done

# Wait for OOMKilled across all three
log_step "Waiting for operators to be OOMKilled..."
elapsed=0
while (( elapsed < 120 )); do
  oom_count=0
  for deploy in "${DEPLOYMENTS[@]}"; do
    status=$(oc get pods -n "$APPS_NS" -o json | jq -r --arg deploy "$deploy" \
      '.items[] | select(.metadata.name | startswith($deploy)) | .status.containerStatuses[]? | "\(.state.waiting.reason // "") \(.lastState.terminated.reason // "")"' 2>/dev/null)
    if echo "$status" | grep -q "OOMKilled\|CrashLoopBackOff"; then
      (( ++oom_count ))
    fi
  done
  if (( oom_count >= ${#DEPLOYMENTS[@]} )); then
    log_step "OOMKilled/CrashLoopBackOff detected on $oom_count operators."
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

if (( oom_count < ${#DEPLOYMENTS[@]} )); then
  log_step "WARN: Not enough operators OOMKilled within timeout (found: $oom_count). Check manually."
fi

log_step "Multi-operator OOM injected."
log_step "Run MCP tools to validate, then run multi-operator-oom-teardown.sh to restore."
