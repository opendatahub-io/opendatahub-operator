#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
TRUSTYAI_DEPLOY="trustyai-service-operator-controller-manager"
DASHBOARD_DEPLOY="odh-dashboard"
BAD_TAG="v999-does-not-exist"

log_step "=== Scenario 26: OOM Plus Wrong Tag (RHOAIENG-53745 + troubleshooting.md) — Setup ==="

# Scale down the ODH operator so it doesn't revert our changes
log_step "Scaling down ODH operator to prevent reconciliation..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=0
sleep 5

# Backup both deployments
log_step "Backing up $TRUSTYAI_DEPLOY..."
backup_resource deployment "$TRUSTYAI_DEPLOY" "$APPS_NS" "$BACKUP_DIR/26-${TRUSTYAI_DEPLOY}.json"
log_step "Backing up $DASHBOARD_DEPLOY..."
backup_resource deployment "$DASHBOARD_DEPLOY" "$APPS_NS" "$BACKUP_DIR/26-${DASHBOARD_DEPLOY}.json"

# Failure 1: Patch TrustyAI memory to 5Mi → OOMKilled
TRUSTYAI_INDEX=$(oc get deployment "$TRUSTYAI_DEPLOY" -n "$APPS_NS" -o json | \
  jq '.spec.template.spec.containers | to_entries[] | select(.value.name == "manager") | .key')

if [[ -z "$TRUSTYAI_INDEX" ]]; then
  log_step "ERROR: Container 'manager' not found in deployment '$TRUSTYAI_DEPLOY'"
  exit 1
fi

log_step "Patching $TRUSTYAI_DEPLOY memory to 5Mi (will OOMKill)..."
oc patch deployment "$TRUSTYAI_DEPLOY" -n "$APPS_NS" --type=json \
  -p '[{"op":"replace","path":"/spec/template/spec/containers/'"$TRUSTYAI_INDEX"'/resources/limits/memory","value":"5Mi"},{"op":"replace","path":"/spec/template/spec/containers/'"$TRUSTYAI_INDEX"'/resources/requests/memory","value":"5Mi"}]'

# Failure 2: Patch dashboard image to nonexistent tag → ImagePullBackOff
CURRENT_IMAGE=$(oc get deployment "$DASHBOARD_DEPLOY" -n "$APPS_NS" -o json | \
  jq -r '.spec.template.spec.containers[] | select(.name == "odh-dashboard") | .image')
IMAGE_NO_DIGEST="${CURRENT_IMAGE%@*}"
REPO="${IMAGE_NO_DIGEST%:*}"
BAD_IMAGE="${REPO}:${BAD_TAG}"
log_step "Patching $DASHBOARD_DEPLOY image to $BAD_IMAGE (will ImagePullBackOff)..."
oc set image "deployment/$DASHBOARD_DEPLOY" -n "$APPS_NS" "odh-dashboard=$BAD_IMAGE"

# Wait for both failures to manifest
log_step "Waiting for both failures to manifest..."
elapsed=0
oom_detected=false
pull_detected=false
while (( elapsed < 120 )); do
  # Check TrustyAI for OOMKilled/CrashLoopBackOff
  trustyai_status=$(oc get pods -n "$APPS_NS" -o json | jq -r --arg deploy "$TRUSTYAI_DEPLOY" \
    '.items[] | select(.metadata.name | startswith($deploy)) | .status.containerStatuses[]? | "\(.state.waiting.reason // "") \(.lastState.terminated.reason // "")"' 2>/dev/null)
  if echo "$trustyai_status" | grep -q "OOMKilled\|CrashLoopBackOff"; then
    oom_detected=true
  fi

  # Check dashboard for ImagePullBackOff
  dashboard_status=$(oc get pods -n "$APPS_NS" -l app=odh-dashboard -o jsonpath='{range .items[*]}{range .status.containerStatuses[*]}{.state.waiting.reason}{"\n"}{end}{end}' 2>/dev/null)
  if echo "$dashboard_status" | grep -q "ImagePullBackOff\|ErrImagePull"; then
    pull_detected=true
  fi

  if [[ "$oom_detected" == "true" ]] && [[ "$pull_detected" == "true" ]]; then
    log_step "Both failures detected: TrustyAI OOMKilled + Dashboard ImagePullBackOff."
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

if [[ "$oom_detected" != "true" ]]; then
  log_step "WARN: TrustyAI OOMKilled not detected within timeout."
fi
if [[ "$pull_detected" != "true" ]]; then
  log_step "WARN: Dashboard ImagePullBackOff not detected within timeout."
fi

log_step "Multi-failure injected: OOM + wrong image tag."
log_step "Run MCP tools to validate, then run oom-plus-wrong-tag-teardown.sh to restore."
