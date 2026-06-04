#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common.sh"

require_tools oc

APPS_NS=$(discover_apps_namespace)
OPERATOR_NS=$(discover_operator_namespace)
OPERATOR_DEPLOY="opendatahub-operator-controller-manager"
CRB_NAME="model-registry-operator-manager-rolebinding"
COMPONENT_DEPLOY="model-registry-operator-controller-manager"

log_step "=== Scenario 22: RBAC Blocks Reconciliation — Teardown ==="

# Ensure operator is always restored even if rollback fails
trap 'oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=3 2>/dev/null || true' EXIT

# Recreate the ClusterRoleBinding
log_step "Recreating ClusterRoleBinding $CRB_NAME..."
oc apply -f - <<EOF
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ${CRB_NAME}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: model-registry-operator-manager-role
subjects:
  - kind: ServiceAccount
    name: model-registry-operator-controller-manager
    namespace: ${APPS_NS}
EOF

# Restart model-registry controller to pick up restored permissions
log_step "Restarting $COMPONENT_DEPLOY..."
oc rollout restart deployment "$COMPONENT_DEPLOY" -n "$APPS_NS"

# Scale operator back up
log_step "Scaling $OPERATOR_DEPLOY back to 3 replicas..."
oc scale deployment "$OPERATOR_DEPLOY" -n "$OPERATOR_NS" --replicas=3

trap - EXIT

wait_for_deployment_ready "$OPERATOR_NS" "$OPERATOR_DEPLOY" "180s"
wait_for_deployment_ready "$APPS_NS" "$COMPONENT_DEPLOY" "180s"

log_step "RBAC restored, model-registry and operator running. Scenario 22 teardown complete."
