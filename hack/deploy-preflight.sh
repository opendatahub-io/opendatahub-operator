#!/usr/bin/env bash
set -euo pipefail

KUBECTL="${KUBECTL:-kubectl}"
OPERATOR_NAMESPACE="${OPERATOR_NAMESPACE:-opendatahub-operator-system}"
ODH_PLATFORM_TYPE="${ODH_PLATFORM_TYPE:-OpenDataHub}"
DEPLOY_ALLOW_CONFLICTING_OPERATORS="${DEPLOY_ALLOW_CONFLICTING_OPERATORS:-false}"

case "${ODH_PLATFORM_TYPE}" in
rhoai|RHOAI)
  current_operator_name=rhods-operator
  is_rhoai=true
  ;;
*)
  current_operator_name=opendatahub-operator-controller-manager
  is_rhoai=false
  ;;
esac

case "${DEPLOY_ALLOW_CONFLICTING_OPERATORS}" in
true|1)
  echo "WARNING: conflicting operator checks are disabled."
  exit 0
  ;;
false|0)
  ;;
*)
  echo "ERROR: DEPLOY_ALLOW_CONFLICTING_OPERATORS must be true, false, 1, or 0." >&2
  exit 2
  ;;
esac

if ! command -v "${KUBECTL}" >/dev/null 2>&1; then
  echo "ERROR: ${KUBECTL} was not found in PATH." >&2
  exit 2
fi

# Return NotFound-like errors as an absent resource, but fail on auth or
# connectivity errors so the guard cannot silently allow a risky deployment.
optional_get() {
  local output

  if output=$("${KUBECTL}" get "$@" 2>&1); then
    printf '%s\n' "${output}"
    return 0
  fi

  case "${output}" in
  *NotFound*|*"not found"*|*"doesn't have a resource type"*)
    return 1
    ;;
  *)
    echo "ERROR: failed to inspect the cluster with ${KUBECTL} get $*." >&2
    printf '%s\n' "${output}" >&2
    return 2
    ;;
  esac
}

has_resource() {
  local status

  if optional_get "$@" >/dev/null; then
    return 0
  else
    status=$?
    if ((status != 1)); then
      exit "${status}"
    fi
    return 1
  fi
}

conflicts=()
has_existing_operator=false

if has_resource deployment "${current_operator_name}" \
  -n "${OPERATOR_NAMESPACE}" -o name; then
  has_existing_operator=true
fi

if deployments=$(optional_get deployment -A \
  -o 'custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name' --no-headers); then
  while read -r namespace name; do
    [[ -z "${name}" ]] && continue
    case "${name}" in
    rhods-operator)
      if [[ "${is_rhoai}" != true || "${namespace}" != "${OPERATOR_NAMESPACE}" ]]; then
        conflicts+=("Deployment ${name} in namespace ${namespace}")
      fi
      ;;
    opendatahub-operator-controller-manager)
      if [[ "${is_rhoai}" == true || "${namespace}" != "${OPERATOR_NAMESPACE}" ]]; then
        conflicts+=("Deployment ${name} in namespace ${namespace}")
      fi
      ;;
    esac
  done <<< "${deployments}"
else
  status=$?
  if ((status != 1)); then
    exit "${status}"
  fi
fi

if subscriptions=$(optional_get subscription -A \
  -o 'custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name' --no-headers); then
  while read -r namespace name; do
    case "${name}" in
    rhods-operator)
      conflicts+=("Subscription ${name} in namespace ${namespace}")
      ;;
    esac
  done <<< "${subscriptions}"
else
  status=$?
  if ((status != 1)); then
    exit "${status}"
  fi
fi

if csvs=$(optional_get csv -A \
  -o 'custom-columns=NAMESPACE:.metadata.namespace,NAME:.metadata.name' --no-headers); then
  while read -r namespace name; do
    case "${name}" in
    rhods-operator*)
      conflicts+=("CSV ${name} in namespace ${namespace}")
      ;;
    esac
  done <<< "${csvs}"
else
  status=$?
  if ((status != 1)); then
    exit "${status}"
  fi
fi

if [[ "${has_existing_operator}" != true ]]; then
  for crd in \
    datascienceclusters.datasciencecluster.opendatahub.io \
    dscinitializations.dscinitialization.opendatahub.io; do
    if has_resource crd "${crd}" -o name; then
      conflicts+=("CRD ${crd}")
    fi
  done

  for resource in \
    datascienceclusters.datasciencecluster.opendatahub.io \
    dscinitializations.dscinitialization.opendatahub.io; do
    if resources=$(optional_get "${resource}" -A -o name); then
      if [[ -n "${resources}" ]]; then
        conflicts+=("Existing ${resource} resources: ${resources}")
      fi
    else
      status=$?
      if ((status != 1)); then
        exit "${status}"
      fi
    fi
  done
fi

is_opendatahub_webhook() {
  case "$1" in
  *opendatahub-operator*|*odh-*|*model-registry*|*workbench*|*kubeflow*|*trainer*)
    return 0
    ;;
  esac
  return 1
}

is_rhoai_webhook() {
  case "$1" in
  *rhods*|*datasciencecluster*|*dscinitialization*)
    return 0
    ;;
  esac
  return 1
}

if webhooks=$(optional_get validatingwebhookconfiguration,mutatingwebhookconfiguration -o name); then
  while IFS= read -r webhook; do
    if [[ "${is_rhoai}" == true ]]; then
      if is_opendatahub_webhook "${webhook}"; then
        conflicts+=("${webhook}")
      fi
    elif is_rhoai_webhook "${webhook}" || \
      { [[ "${has_existing_operator}" != true ]] && is_opendatahub_webhook "${webhook}"; }; then
      conflicts+=("${webhook}")
    fi
  done <<< "${webhooks}"
else
  status=$?
  if ((status != 1)); then
    exit "${status}"
  fi
fi

if ((${#conflicts[@]} > 0)); then
  echo "ERROR: an existing RHOAI/ODH installation was detected." >&2
  echo "make deploy was stopped before applying manifests." >&2
  printf '  %s\n' "${conflicts[@]}" >&2
  echo "Use a clean cluster or uninstall the existing operator first." >&2
  echo "For an intentional mixed-cluster test, set DEPLOY_ALLOW_CONFLICTING_OPERATORS=true." >&2
  exit 1
fi

echo "Deploy preflight passed: no conflicting RHOAI/ODH installation detected."
