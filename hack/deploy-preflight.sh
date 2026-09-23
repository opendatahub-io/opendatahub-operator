#!/usr/bin/env bash
set -euo pipefail

KUBECTL="${KUBECTL:-kubectl}"
OPERATOR_NAMESPACE="${OPERATOR_NAMESPACE:-opendatahub-operator-system}"
DEPLOY_ALLOW_CONFLICTING_OPERATORS="${DEPLOY_ALLOW_CONFLICTING_OPERATORS:-false}"

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
    exit 2
    ;;
  esac
}

has_resource() {
  optional_get "$@" >/dev/null
}

conflicts=()
has_existing_odh=false

if has_resource deployment opendatahub-operator-controller-manager \
  -n "${OPERATOR_NAMESPACE}" -o name; then
  has_existing_odh=true
fi

for namespace in openshift-operators redhat-ods-operator; do
  if has_resource subscription rhods-operator -n "${namespace}" -o name; then
    conflicts+=("Subscription rhods-operator in namespace ${namespace}")
  fi
  if has_resource deployment rhods-operator -n "${namespace}" -o name; then
    conflicts+=("Deployment rhods-operator in namespace ${namespace}")
  fi
done

if csvs=$(optional_get csv -A -o name); then
  while IFS= read -r csv; do
    case "${csv}" in
    *rhods-operator*)
      conflicts+=("${csv}")
      ;;
    esac
  done <<< "${csvs}"
fi

if [[ "${has_existing_odh}" != true ]]; then
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
    if resources=$(optional_get "${resource}" -A -o name) && [[ -n "${resources}" ]]; then
      conflicts+=("Existing ${resource} resources: ${resources}")
    fi
  done
fi

if webhooks=$(optional_get validatingwebhookconfiguration,mutatingwebhookconfiguration -o name); then
  while IFS= read -r webhook; do
    case "${webhook}" in
    *rhods*|*datasciencecluster*|*dscinitialization*|*odh-*|*model-registry*|*workbench*|*kubeflow*|*trainer*)
      if [[ "${has_existing_odh}" != true || "${webhook}" != *opendatahub-operator* ]]; then
        conflicts+=("${webhook}")
      fi
      ;;
    esac
  done <<< "${webhooks}"
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
