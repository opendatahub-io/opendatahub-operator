#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
DEPLOYMENT="feast-operator-controller-manager"
CONTAINER="manager"

log_step "=== Scenario 18: Panic Crash — Setup ==="

# Scale down the ODH operator so it doesn't revert our change
log_step "Scaling down ODH operator to prevent reconciliation..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=0
sleep 5

# Backup original deployment
backup_resource deployment "$DEPLOYMENT" "$APPS_NS" "$BACKUP_DIR/18-${DEPLOYMENT}.json"

# Find the index of the target container
CONTAINER_INDEX=$(oc get deployment "$DEPLOYMENT" -n "$APPS_NS" -o json | \
  jq '.spec.template.spec.containers | to_entries[] | select(.value.name == "'"$CONTAINER"'") | .key')

if [[ -z "$CONTAINER_INDEX" ]]; then
  log_step "ERROR: Could not find container $CONTAINER in $DEPLOYMENT"
  exit 1
fi

# Patch container command to a script that prints a fake panic stack trace then exits
log_step "Patching $DEPLOYMENT/$CONTAINER (index $CONTAINER_INDEX) to crash with panic-like output..."
oc patch deployment "$DEPLOYMENT" -n "$APPS_NS" --type=json \
  -p '[{"op":"add","path":"/spec/template/spec/containers/'"$CONTAINER_INDEX"'/command","value":["sh","-c","echo \"panic: runtime error: invalid memory address or nil pointer dereference\" >&2; echo \"[signal SIGSEGV: segmentation violation]\" >&2; echo \"\" >&2; echo \"goroutine 1 [running]:\" >&2; echo \"main.main()\" >&2; echo \"  /workspace/cmd/main.go:105 +0x1a4\" >&2; exit 2"]}]'

# Wait for CrashLoopBackOff
log_step "Waiting for container to enter CrashLoopBackOff..."
elapsed=0
while (( elapsed < 120 )); do
  waiting_reason=$(oc get pods -n "$APPS_NS" -l control-plane=controller-manager,app.kubernetes.io/name=feast-operator \
    -o jsonpath='{range .items[*]}{.status.containerStatuses[?(@.name=="'"$CONTAINER"'")].state.waiting.reason}{"\n"}{end}' 2>/dev/null || echo "")
  if echo "$waiting_reason" | grep -q "CrashLoopBackOff"; then
    log_step "Container is in CrashLoopBackOff."
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

if ! echo "$waiting_reason" | grep -q "CrashLoopBackOff"; then
  log_step "WARN: Container did not enter CrashLoopBackOff within timeout (current: $waiting_reason)"
fi

log_step "Panic crash failure injected."
log_step "Run MCP tools to validate, then run panic-crash-teardown.sh to restore."
