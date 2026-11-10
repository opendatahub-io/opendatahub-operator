#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PREFLIGHT="${SCRIPT_DIR}/deploy-preflight.sh"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

MOCK_KUBECTL="${TMP_DIR}/kubectl"
cat > "${MOCK_KUBECTL}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

not_found() {
  echo "Error from server (NotFound): resource was not found" >&2
  exit 1
}

case "${MOCK_SCENARIO:-clean}:$*" in
conflict:*"get subscription rhods-operator"*)
  echo "subscription.operators.coreos.com/rhods-operator"
  ;;
conflict:*"get deployment rhods-operator"*)
  echo "deployment.apps/rhods-operator"
  ;;
rhoai:*"get deployment rhods-operator -n redhat-ods-operator"*)
  echo "deployment.apps/rhods-operator"
  ;;
conflict:*"get deployment -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*)
  echo "openshift-operators rhods-operator"
  ;;
odh-other:*"get deployment -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*)
  printf 'opendatahub-operator-system opendatahub-operator-controller-manager\nother-namespace rhods-operator\n'
  ;;
odh:*"get deployment -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*)
  echo "opendatahub-operator-system opendatahub-operator-controller-manager"
  ;;
rhoai:*"get deployment -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*)
  echo "redhat-ods-operator rhods-operator"
  ;;
rhoai-olm:*"get deployment -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*)
  echo "redhat-ods-operator rhods-operator"
  ;;
clean:*"get deployment -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*|webhook:*"get deployment -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*)
  ;;
webhook:*"get validatingwebhookconfiguration,mutatingwebhookconfiguration"*)
  echo "validatingwebhookconfiguration.admissionregistration.k8s.io/datasciencecluster-v1-validator.opendatahub.io-f9wxw"
  ;;
odh:*"get deployment opendatahub-operator-controller-manager"*)
  echo "deployment.apps/opendatahub-operator-controller-manager"
  ;;
odh:*"get validatingwebhookconfiguration,mutatingwebhookconfiguration"*)
  echo "validatingwebhookconfiguration.admissionregistration.k8s.io/opendatahub-operator-validating-webhook-configuration"
  ;;
rhoai:*"get validatingwebhookconfiguration,mutatingwebhookconfiguration"*)
  echo "validatingwebhookconfiguration.admissionregistration.k8s.io/datasciencecluster-v1-validator.opendatahub.io-f9wxw"
  ;;
rhoai-olm:*"get subscription -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*)
  echo "openshift-operators rhods-operator"
  ;;
rhoai-olm:*"get csv -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*)
  echo "openshift-operators rhods-operator.3.5.1"
  ;;
clean:*"get subscription -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*|conflict:*"get subscription -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*|webhook:*"get subscription -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*|odh:*"get subscription -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*|odh-other:*"get subscription -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*|rhoai:*"get subscription -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*)
  ;;
clean:*"get csv -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*|conflict:*"get csv -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*|webhook:*"get csv -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*|odh:*"get csv -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*|odh-other:*"get csv -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*|rhoai:*"get csv -A -o custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name --no-headers"*)
  ;;
clean:*"get csv -A -o name"*|conflict:*"get csv -A -o name"*|webhook:*"get csv -A -o name"*|odh:*"get csv -A -o name"*)
  ;;
clean:*"get datascienceclusters.datasciencecluster.opendatahub.io -A -o name"*|conflict:*"get datascienceclusters.datasciencecluster.opendatahub.io -A -o name"*|webhook:*"get datascienceclusters.datasciencecluster.opendatahub.io -A -o name"*|odh:*"get datascienceclusters.datasciencecluster.opendatahub.io -A -o name"*|odh-other:*"get datascienceclusters.datasciencecluster.opendatahub.io -A -o name"*|rhoai:*"get datascienceclusters.datasciencecluster.opendatahub.io -A -o name"*|rhoai-olm:*"get datascienceclusters.datasciencecluster.opendatahub.io -A -o name"*)
  ;;
clean:*"get dscinitializations.dscinitialization.opendatahub.io -A -o name"*|conflict:*"get dscinitializations.dscinitialization.opendatahub.io -A -o name"*|webhook:*"get dscinitializations.dscinitialization.opendatahub.io -A -o name"*|odh:*"get dscinitializations.dscinitialization.opendatahub.io -A -o name"*|odh-other:*"get dscinitializations.dscinitialization.opendatahub.io -A -o name"*|rhoai:*"get dscinitializations.dscinitialization.opendatahub.io -A -o name"*|rhoai-olm:*"get dscinitializations.dscinitialization.opendatahub.io -A -o name"*)
  ;;
clean:*"get validatingwebhookconfiguration,mutatingwebhookconfiguration -o name"*|conflict:*"get validatingwebhookconfiguration,mutatingwebhookconfiguration -o name"*|odh:*"get validatingwebhookconfiguration,mutatingwebhookconfiguration -o name"*|odh-other:*"get validatingwebhookconfiguration,mutatingwebhookconfiguration -o name"*|rhoai-olm:*"get validatingwebhookconfiguration,mutatingwebhookconfiguration -o name"*)
  ;;
clean:*"get subscription rhods-operator"*|clean:*"get deployment rhods-operator"*|clean:*"get deployment opendatahub-operator-controller-manager"*|webhook:*"get subscription rhods-operator"*|webhook:*"get deployment rhods-operator"*|webhook:*"get deployment opendatahub-operator-controller-manager"*|odh:*"get subscription rhods-operator"*|odh:*"get deployment rhods-operator"*|rhoai:*"get deployment opendatahub-operator-controller-manager"*)
  not_found
  ;;
*"get subscription rhods-operator"*|*"get deployment rhods-operator"*|*"get deployment opendatahub-operator-controller-manager"*|*"get crd datascienceclusters"*|*"get crd dscinitializations"*)
  not_found
  ;;
*)
  printf 'Unexpected kubectl invocation: %s\n' "$*" >&2
  exit 2
  ;;
esac
EOF
chmod +x "${MOCK_KUBECTL}"

run_preflight() {
  local scenario="$1"
  local allow="${2:-false}"
  local platform="${3:-OpenDataHub}"
  local operator_namespace=opendatahub-operator-system
  local output status

  if [[ "${platform}" == rhoai ]]; then
    operator_namespace=redhat-ods-operator
  fi

  if output=$(MOCK_SCENARIO="${scenario}" KUBECTL="${MOCK_KUBECTL}" \
    ODH_PLATFORM_TYPE="${platform}" OPERATOR_NAMESPACE="${operator_namespace}" \
    DEPLOY_ALLOW_CONFLICTING_OPERATORS="${allow}" "${PREFLIGHT}" 2>&1); then
    status=0
  else
    status=$?
  fi

  printf '%s\n' "${status}" "${output}"
}

clean_result=$(run_preflight clean)
[[ "${clean_result}" == 0$'\n'*"Deploy preflight passed"* ]]

failure_result=$(run_preflight failure)
[[ "${failure_result}" == 2$'\n'*"failed to inspect the cluster"* ]]

conflict_result=$(run_preflight conflict)
[[ "${conflict_result}" == 1$'\n'*"existing RHOAI/ODH installation"* ]]

webhook_result=$(run_preflight webhook)
[[ "${webhook_result}" == 1$'\n'*"existing RHOAI/ODH installation"* ]]

override_result=$(run_preflight conflict true)
[[ "${override_result}" == 0$'\n'*"checks are disabled"* ]]

odh_result=$(run_preflight odh)
[[ "${odh_result}" == 0$'\n'*"Deploy preflight passed"* ]]

rhoai_result=$(run_preflight rhoai false rhoai)
[[ "${rhoai_result}" == 0$'\n'*"Deploy preflight passed"* ]]

odh_other_result=$(run_preflight odh-other)
[[ "${odh_other_result}" == 1$'\n'*"existing RHOAI/ODH installation"* ]]

rhoai_olm_result=$(run_preflight rhoai-olm false rhoai)
[[ "${rhoai_olm_result}" == 1$'\n'*"existing RHOAI/ODH installation"* ]]

echo "deploy-preflight tests passed"
