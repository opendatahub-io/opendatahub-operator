#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)

# Safeguard: the cascading-failure-networkpolicy.yaml uses podSelector: {} with
# policyTypes Ingress+Egress, which blocks ALL pods in the target namespace.
# Only allow this in known test/dev namespaces to prevent accidental production impact.
ALLOWED_NS_PATTERN="^(opendatahub|odh-.+|redhat-ods-applications)$"
if [[ ! "$APPS_NS" =~ $ALLOWED_NS_PATTERN ]]; then
  if [[ "${FORCE_DENY_ALL:-}" != "true" ]]; then
    log_step "ERROR: Refusing to apply deny-all NetworkPolicy to namespace '$APPS_NS'."
    log_step "Resolved from DSCI or ODH_APPS_NAMESPACE=${ODH_APPS_NAMESPACE:-<unset>}."
    log_step "This namespace does not match the allowed pattern: $ALLOWED_NS_PATTERN"
    log_step "Set FORCE_DENY_ALL=true to override this safeguard."
    exit 1
  fi
  log_step "WARN: Namespace '$APPS_NS' is not a standard test namespace. Proceeding due to FORCE_DENY_ALL=true."
fi

log_step "=== Scenario: Cascading Failure — Setup ==="

log_step "Applying deny-all NetworkPolicy to $APPS_NS namespace..."
log_step "  (podSelector: {} with policyTypes: [Ingress, Egress] — blocks all pods)"
oc apply -f "$SCRIPT_DIR/cascading-failure-networkpolicy.yaml" -n "$APPS_NS"

log_step "NetworkPolicy applied. Waiting for cascade to propagate..."
sleep 40

log_step "Verifying cascading failure state..."
UNREADY=$(oc get deployments -n "$APPS_NS" -o json 2>/dev/null | \
  jq -r '.items[] | select(.status.replicas != .status.availableReplicas) | .metadata.name' || true)
log_step "Unready deployments: ${UNREADY:-none}"

log_step "Cascading failure scenario setup complete."
