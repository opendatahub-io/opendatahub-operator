#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
DEPLOYMENT="odh-dashboard"
BAD_TAG="v999-does-not-exist"

log_step "=== Scenario 20: Dashboard Sidecar ImagePull — Setup ==="

# Scale down the ODH operator so it doesn't revert our image changes
log_step "Scaling down ODH operator to prevent reconciliation..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=0
sleep 5

# Backup original deployment
backup_resource deployment "$DEPLOYMENT" "$APPS_NS" "$BACKUP_DIR/20-${DEPLOYMENT}.json"


SIDECARS=("model-registry-ui" "gen-ai-ui" "maas-ui" "mlflow-ui")

for sidecar in "${SIDECARS[@]}"; do
  INDEX=$(oc get deployment "$DEPLOYMENT" -n "$APPS_NS" -o json | \
    jq '.spec.template.spec.containers | to_entries[] | select(.value.name == "'"$sidecar"'") | .key')
  if [[ -z "$INDEX" ]]; then
    log_step "ERROR: Required sidecar $sidecar was not found in $DEPLOYMENT"
    exit 1
  fi
  CURRENT_IMAGE=$(oc get deployment "$DEPLOYMENT" -n "$APPS_NS" -o jsonpath='{.spec.template.spec.containers['"$INDEX"'].image}')
  IMAGE_NO_DIGEST="${CURRENT_IMAGE%@*}"
  REPO="${IMAGE_NO_DIGEST%:*}"
  BAD_IMAGE="${REPO}:${BAD_TAG}"
  log_step "Patching $sidecar (index $INDEX) to use image: $BAD_IMAGE"
  oc set image "deployment/$DEPLOYMENT" -n "$APPS_NS" "$sidecar=$BAD_IMAGE"
done

# Wait for ImagePullBackOff
log_step "Waiting for sidecar containers to enter ImagePullBackOff..."
elapsed=0
while (( elapsed < 120 )); do
  pull_errors=$(oc get pods -n "$APPS_NS" -l app=odh-dashboard \
    -o jsonpath='{range .items[*]}{range .status.containerStatuses[*]}{.state.waiting.reason}{"\n"}{end}{end}' 2>/dev/null | grep -c "ImagePullBackOff\|ErrImagePull" || true)
  if (( pull_errors >= 2 )); then
    log_step "Multiple containers in ImagePullBackOff ($pull_errors found)."
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

if (( pull_errors < 2 )); then
  log_step "WARN: Not enough containers in ImagePullBackOff within timeout (found: $pull_errors)"
fi

log_step "Dashboard sidecar image pull failure injected."
log_step "Run MCP tools to validate, then run dashboard-sidecar-imagepull-teardown.sh to restore."
