#!/usr/bin/env bash
# Patch OLM Subscription with RELATED_IMAGE_* env vars from the override env file.
# OLM reconciles the Deployment to match the Subscription config.
# Requires: kubectl, jq
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
ENV_FILE="${ROOT_DIR}/opt/related-images-override.env"
NAMESPACE="${OPERATOR_NAMESPACE:-opendatahub-operator}"
OPERATOR_PACKAGE="${OPERATOR_PACKAGE:-opendatahub-operator}"

if [[ ! -f "$ENV_FILE" ]]; then
    echo "No override env file found at $ENV_FILE"
    echo "Run 'make generate-image-overrides' first"
    exit 1
fi

if [[ ! -s "$ENV_FILE" ]]; then
    echo "Override env file is empty, nothing to apply"
    exit 0
fi

# Find the Subscription matching the operator package
SUB_JSON=$(kubectl get subscriptions.operators.coreos.com -n "$NAMESPACE" -o json 2>/dev/null) || true
SUB_NAMES=$(echo "$SUB_JSON" | jq -r --arg pkg "$OPERATOR_PACKAGE" '.items[] | select(.spec.name == $pkg) | .metadata.name' 2>/dev/null) || true
SUB_COUNT=$(echo "$SUB_NAMES" | grep -c . 2>/dev/null) || true

if [[ "$SUB_COUNT" -eq 0 || -z "$SUB_NAMES" ]]; then
    echo "No Subscription for package '$OPERATOR_PACKAGE' found in namespace $NAMESPACE, skipping OLM image overrides"
    exit 0
fi
if [[ "$SUB_COUNT" -gt 1 ]]; then
    echo "ERROR: multiple Subscriptions for package '$OPERATOR_PACKAGE' found in namespace $NAMESPACE: $SUB_NAMES"
    exit 1
fi

SUB_NAME="$SUB_NAMES"
echo "Found Subscription: $SUB_NAME in namespace $NAMESPACE"

# Build JSON patch from env file using jq
PATCH=$(jq -n --rawfile envfile "$ENV_FILE" '
  [ $envfile | split("\n")[] | select(length > 0 and startswith("#") | not) |
    capture("^(?<name>[^=]+)=(?<value>.+)$") |
    {name: .name, value: .value}
  ] | {spec: {config: {env: .}}}
')

# Patch the Subscription
echo "Patching Subscription $SUB_NAME with image overrides..."
kubectl patch subscription "$SUB_NAME" -n "$NAMESPACE" --type=merge -p "$PATCH"

# Wait for rollout
echo "Waiting for Deployment rollout..."
DEPLOY_NAME=$(kubectl get deployment -n "$NAMESPACE" -l control-plane=controller-manager -o jsonpath='{.items[0].metadata.name}' 2>/dev/null) || true
if [[ -n "$DEPLOY_NAME" ]]; then
    kubectl rollout status "deployment/${DEPLOY_NAME}" -n "$NAMESPACE" --timeout=120s
else
    echo "ERROR: could not find controller-manager deployment in namespace $NAMESPACE"
    exit 1
fi

echo "Image overrides applied via Subscription"
