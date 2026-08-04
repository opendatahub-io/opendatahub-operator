#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
DEPLOYMENT="odh-dashboard"
SIDECAR="kube-rbac-proxy"

log_step "=== Scenario 19: Sidecar Container Crash — Setup ==="

# Scale down the ODH operator so it doesn't revert our change
log_step "Scaling down ODH operator to prevent reconciliation..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=0
sleep 5

# Backup original deployment
backup_resource deployment "$DEPLOYMENT" "$APPS_NS" "$BACKUP_DIR/19-${DEPLOYMENT}.json"

# Find the index of the kube-rbac-proxy container
SIDECAR_INDEX=$(oc get deployment "$DEPLOYMENT" -n "$APPS_NS" -o json | \
  jq '.spec.template.spec.containers | to_entries[] | select(.value.name == "'"$SIDECAR"'") | .key')

if [[ -z "$SIDECAR_INDEX" ]]; then
  log_step "ERROR: Could not find container $SIDECAR in $DEPLOYMENT"
  exit 1
fi
log_step "Found $SIDECAR at container index $SIDECAR_INDEX"

# Patch the sidecar container command to simulate wrong binary with invalid flag
log_step "Patching $SIDECAR container with invalid flag --secure-listen-address..."
oc patch deployment "$DEPLOYMENT" -n "$APPS_NS" --type=json \
  -p '[{"op":"add","path":"/spec/template/spec/containers/'"$SIDECAR_INDEX"'/command","value":["sh","-c","echo \"flag provided but not defined: -secure-listen-address\" >&2; echo \"Usage of kube-rbac-proxy:\" >&2; exit 1"]}]'

# Wait for CrashLoopBackOff on the sidecar
log_step "Waiting for $SIDECAR container to enter CrashLoopBackOff..."
elapsed=0
while (( elapsed < 120 )); do
  waiting_reason=$(oc get pods -n "$APPS_NS" -l app=odh-dashboard \
    -o jsonpath='{range .items[*]}{.status.containerStatuses[?(@.name=="'"$SIDECAR"'")].state.waiting.reason}{"\n"}{end}' 2>/dev/null || echo "")
  if echo "$waiting_reason" | grep -q "CrashLoopBackOff"; then
    log_step "$SIDECAR container is in CrashLoopBackOff."
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

if ! echo "$waiting_reason" | grep -q "CrashLoopBackOff"; then
  log_step "WARN: $SIDECAR did not enter CrashLoopBackOff within timeout (current: $waiting_reason)"
fi

log_step "Sidecar crash failure injected."
log_step "Run MCP tools to validate, then run sidecar-crash-teardown.sh to restore."
