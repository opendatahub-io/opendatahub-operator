#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

APPS_NS=$(discover_apps_namespace)

log_step "=== Scenario 28: Healthy ImageStream Warnings (RHOAIENG-13921) — Setup ==="

# Check if ImageStream warnings already exist on the cluster (they often do naturally)
IMAGESTREAM_WARNINGS=$(oc get dsc -o jsonpath='{.items[0].status.conditions[?(@.type=="WorkbenchesReady")].message}' 2>/dev/null || echo "")

if echo "$IMAGESTREAM_WARNINGS" | grep -qi "ImageStream\|failed to import"; then
  log_step "ImageStream warnings already present in WorkbenchesReady condition:"
  log_step "  $IMAGESTREAM_WARNINGS"
  log_step "No additional setup needed — cluster already has the false-positive condition."
else
  # Create an ImageStream with a bad tag to generate import warnings
  log_step "Creating ImageStream with invalid tag to generate import warnings..."
  if oc get imagestream scenario-test-notebook -n "$APPS_NS" >/dev/null 2>&1; then
    log_step "ERROR: imagestream/scenario-test-notebook already exists in $APPS_NS."
    exit 1
  fi
  oc create -f - <<EOF
apiVersion: image.openshift.io/v1
kind: ImageStream
metadata:
  name: scenario-test-notebook
  namespace: $APPS_NS
  labels:
    opendatahub.io/notebook-image: "true"
spec:
  tags:
  - name: "latest"
    from:
      kind: DockerImage
      name: "quay.io/opendatahub/nonexistent-notebook:v999-does-not-exist"
    importPolicy:
      importMode: Legacy
EOF
  log_step "Waiting for ImageStream import failure..."
  for _ in {1..12}; do
    IMAGESTREAM_WARNINGS=$(oc get dsc -o jsonpath='{.items[0].status.conditions[?(@.type=="WorkbenchesReady")].message}' 2>/dev/null || echo "")
    if echo "$IMAGESTREAM_WARNINGS" | grep -qi "ImageStream\|failed to import"; then
      log_step "ImageStream warning detected."
      break
    fi
    sleep 5
  done
fi

# Verify all pods are still healthy
UNHEALTHY_PODS=$(oc get pods -n "$APPS_NS" --field-selector='status.phase!=Running,status.phase!=Succeeded' --no-headers 2>/dev/null | wc -l)
if (( UNHEALTHY_PODS == 0 )); then
  log_step "All pods healthy — false-positive condition active (warnings present but no failures)."
else
  log_step "WARN: $UNHEALTHY_PODS unhealthy pods found — this may not be a clean false-positive test."
fi

log_step "Run MCP tools to validate. Agent should report 'Platform Healthy' despite ImageStream warnings."
log_step "Run healthy-imagestream-warnings-teardown.sh to clean up."
