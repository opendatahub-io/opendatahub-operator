#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc jq

CERT_OPERATOR_NS="cert-manager-operator"
CERT_OPERATOR_DEPLOY="cert-manager-operator-controller-manager"

log_step "=== Scenario 01: Missing Dependency — Setup ==="

# Backup cert-manager operator deployment
backup_resource deployment "$CERT_OPERATOR_DEPLOY" "$CERT_OPERATOR_NS" "$BACKUP_DIR/01-cert-manager-operator.json"

# Scale down cert-manager operator
log_step "Scaling $CERT_OPERATOR_DEPLOY to 0 replicas..."
oc scale deployment "$CERT_OPERATOR_DEPLOY" -n "$CERT_OPERATOR_NS" --replicas=0

# Wait for operator pod to terminate
log_step "Waiting for cert-manager operator pod to terminate..."
elapsed=0
while (( elapsed < 60 )); do
  pod_count=$(oc get pods -n "$CERT_OPERATOR_NS" --no-headers 2>/dev/null | grep -c Running || true)
  if (( pod_count == 0 )); then
    break
  fi
  sleep 5
  (( elapsed += 5 ))
done

log_step "cert-manager operator is down. Dependency failure injected."
log_step "Run MCP tools to validate, then run missing-dependency-teardown.sh to restore."
