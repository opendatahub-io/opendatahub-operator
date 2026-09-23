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
webhook:*"get validatingwebhookconfiguration,mutatingwebhookconfiguration"*)
  echo "validatingwebhookconfiguration.admissionregistration.k8s.io/datasciencecluster-v1-validator.opendatahub.io-f9wxw"
  ;;
odh:*"get deployment opendatahub-operator-controller-manager"*)
  echo "deployment.apps/opendatahub-operator-controller-manager"
  ;;
odh:*"get validatingwebhookconfiguration,mutatingwebhookconfiguration"*)
  echo "validatingwebhookconfiguration.admissionregistration.k8s.io/opendatahub-operator-validating-webhook-configuration"
  ;;
*"get subscription rhods-operator"*|*"get deployment rhods-operator"*|*"get deployment opendatahub-operator-controller-manager"*|*"get crd datascienceclusters"*|*"get crd dscinitializations"*)
  not_found
  ;;
*)
  ;;
esac
EOF
chmod +x "${MOCK_KUBECTL}"

run_preflight() {
  local scenario=$1
  local allow=${2:-false}
  local output status

  if output=$(MOCK_SCENARIO="${scenario}" KUBECTL="${MOCK_KUBECTL}" \
    DEPLOY_ALLOW_CONFLICTING_OPERATORS="${allow}" "${PREFLIGHT}" 2>&1); then
    status=0
  else
    status=$?
  fi

  printf '%s\n' "${status}" "${output}"
}

clean_result=$(run_preflight clean)
[[ "${clean_result}" == 0$'\n'*"Deploy preflight passed"* ]]

conflict_result=$(run_preflight conflict)
[[ "${conflict_result}" == 1$'\n'*"existing RHOAI/ODH installation"* ]]

webhook_result=$(run_preflight webhook)
[[ "${webhook_result}" == 1$'\n'*"existing RHOAI/ODH installation"* ]]

override_result=$(run_preflight conflict true)
[[ "${override_result}" == 0$'\n'*"checks are disabled"* ]]

odh_result=$(run_preflight odh)
[[ "${odh_result}" == 0$'\n'*"Deploy preflight passed"* ]]

echo "deploy-preflight tests passed"
