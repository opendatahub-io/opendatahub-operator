#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
DEPLOYMENT="odh-dashboard"
QUOTA_NAME="scenario-tight-quota"

log_step "=== Scenario 14: Quota Exceeded Realistic — Setup ==="

# Calculate current total memory requests in the namespace
TOTAL_MEM_BYTES=$(oc get pods -n "$APPS_NS" -o json 2>/dev/null | jq '
  [.items[] | select(.status.phase == "Running") |
   .spec.containers[].resources.requests.memory // "0" |
   if endswith("Gi") then (rtrimstr("Gi") | tonumber * 1073741824)
   elif endswith("Mi") then (rtrimstr("Mi") | tonumber * 1048576)
   elif endswith("Ki") then (rtrimstr("Ki") | tonumber * 1024)
   else tonumber end
  ] | add // 0')

# Set quota to 80% of current usage so new pods can't fit
QUOTA_MEM_BYTES=$(( TOTAL_MEM_BYTES * 80 / 100 ))
QUOTA_MEM_MI=$(( QUOTA_MEM_BYTES / 1048576 ))

log_step "Current namespace memory requests: $(( TOTAL_MEM_BYTES / 1048576 ))Mi"
log_step "Setting quota to ${QUOTA_MEM_MI}Mi (80% of current usage)"

# Scale down the ODH operator so it doesn't interfere
log_step "Scaling down ODH operator to prevent reconciliation..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=0
sleep 5

# Apply tight quota
log_step "Applying tight ResourceQuota to $APPS_NS..."
cat <<EOF | oc apply -f -
apiVersion: v1
kind: ResourceQuota
metadata:
  name: $QUOTA_NAME
  namespace: $APPS_NS
spec:
  hard:
    requests.memory: "${QUOTA_MEM_MI}Mi"
    limits.memory: "${QUOTA_MEM_MI}Mi"
EOF

# Trigger a pod restart so new pods hit the quota
log_step "Deleting a $DEPLOYMENT pod to trigger reschedule against quota..."
oc delete pod -l app=odh-dashboard -n "$APPS_NS" --wait=false

# Wait for FailedCreate events
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

log_step "Quota exceeded failure injected."
log_step "Run MCP tools to validate, then run quota-exceeded-realistic-teardown.sh to restore."
