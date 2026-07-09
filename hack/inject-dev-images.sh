#!/usr/bin/env bash
#
# inject-dev-images.sh — Patch the deployed operator with RELATED_IMAGE_* env
# vars extracted from bundled module manifests.
#
# Usage:
#   make inject-images
#
# This script discovers RELATED_IMAGE_* env var names from Go source, resolves
# their default image references from the module operator Deployments in the
# opt/ artifacts, and patches the live operator Deployment with those env vars.
#
# The script auto-detects the platform type from the running operator Deployment.
# Override with: OPERATOR_NAMESPACE=<ns> hack/inject-dev-images.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHARTS_ROOT="${DEFAULT_CHARTS_PATH:-${REPO_ROOT}/opt/charts}"
MANIFESTS_ROOT="${DEFAULT_MANIFESTS_PATH:-${REPO_ROOT}/opt/manifests}"

# kubectl or oc
KUBECTL="${KUBECTL:-$(command -v oc 2>/dev/null || command -v kubectl 2>/dev/null)}"

# Detect namespace and deployment name
NS="${OPERATOR_NAMESPACE:-}"
DEPLOY_NAME=""

detect_operator() {
    if [[ -z "${NS}" ]]; then
        for ns in opendatahub-operator-system redhat-ods-operator; do
            if ${KUBECTL} get namespace "${ns}" &>/dev/null; then
                NS="${ns}"
                break
            fi
        done
    fi

    if [[ -z "${NS}" ]]; then
        echo "ERROR: Could not detect operator namespace. Set OPERATOR_NAMESPACE." >&2
        exit 1
    fi

    DEPLOY_NAME=$(${KUBECTL} get deployment -n "${NS}" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
    if [[ -z "${DEPLOY_NAME}" ]]; then
        echo "ERROR: No deployment found in namespace ${NS}." >&2
        exit 1
    fi

    echo "Detected operator: ${NS}/${DEPLOY_NAME}"
}

# Collect RELATED_IMAGE_* env var names from Go source.
collect_image_env_names() {
    rg -o '"(RELATED_IMAGE_[A-Z0-9_]+)"' --no-filename -r '$1' \
        "${REPO_ROOT}/internal/controller/modules/" \
        "${REPO_ROOT}/internal/controller/components/" \
    | sort -u
}

# Extract image references from rendered Deployment manifests in opt/.
# For each module's operator Deployment, we look for container images and
# env vars that match RELATED_IMAGE_* patterns.
collect_images_from_artifacts() {
    local -n result_ref=$1
    local search_dirs=()

    if [[ -d "${MANIFESTS_ROOT}" ]]; then
        while IFS= read -r -d '' d; do
            search_dirs+=("$d")
        done < <(find "${MANIFESTS_ROOT}" -maxdepth 1 -mindepth 1 -type d -print0)
    fi

    for dir in "${search_dirs[@]}"; do
        while IFS= read -r -d '' yaml_file; do
            extract_deployment_images "${yaml_file}" result_ref
        done < <(find "${dir}" -name '*.yaml' -path '*/manager/*' -print0 2>/dev/null || true)
    done

    if [[ -d "${CHARTS_ROOT}" ]]; then
        while IFS= read -r -d '' chart_dir; do
            local values_file="${chart_dir}/values.yaml"
            if [[ -f "${values_file}" ]]; then
                extract_helm_values_images "${values_file}" result_ref
            fi
        done < <(find "${CHARTS_ROOT}" -maxdepth 1 -mindepth 1 -type d -print0)
    fi
}

extract_deployment_images() {
    local file=$1
    local -n imgs=$2

    while IFS= read -r line; do
        local name value
        name=$(echo "${line}" | rg -o 'name:\s*(RELATED_IMAGE_[A-Z0-9_]+)' -r '$1' 2>/dev/null || true)
        if [[ -n "${name}" ]]; then
            value=$(echo "${line}" | rg -o 'value:\s*(.+)' -r '$1' 2>/dev/null || true)
            if [[ -n "${value}" ]]; then
                imgs["${name}"]="${value}"
            fi
        fi
    done < <(rg 'RELATED_IMAGE_' "${file}" 2>/dev/null || true)

    while IFS= read -r line; do
        local image
        image=$(echo "${line}" | rg -o 'image:\s*(\S+)' -r '$1' 2>/dev/null || true)
        if [[ -n "${image}" && "${image}" != *"{"* ]]; then
            local basename
            basename=$(echo "${image}" | rg -o '/([^/:@]+)[@:]' -r '$1' 2>/dev/null || echo "${image}")
            local env_name="RELATED_IMAGE_$(echo "${basename}" | tr '[:lower:]-' '[:upper:]_')"
            if [[ -z "${imgs["${env_name}"]+_}" ]]; then
                imgs["${env_name}"]="${image}"
            fi
        fi
    done < <(rg 'image:' "${file}" 2>/dev/null || true)
}

extract_helm_values_images() {
    local file=$1
    local -n imgs=$2

    while IFS= read -r line; do
        local key value
        key=$(echo "${line}" | rg -o '^\s*(\S+):\s+"?([^"]+)"?' -r '$1' 2>/dev/null || true)
        value=$(echo "${line}" | rg -o '^\s*\S+:\s+"?([^"]+)"?' -r '$1' 2>/dev/null || true)
        if [[ "${key}" == RELATED_IMAGE_* && -n "${value}" ]]; then
            imgs["${key}"]="${value}"
        fi
    done < <(rg 'RELATED_IMAGE_' "${file}" 2>/dev/null || true)
}

build_and_apply_patch() {
    local -n env_names=$1
    local -n artifact_images=$2

    local patch_entries=()
    local found=0
    local skipped=0

    for name in "${env_names[@]}"; do
        local value="${artifact_images["${name}"]:-}"
        if [[ -z "${value}" ]]; then
            echo "  SKIP: ${name} (no image found in artifacts)"
            ((skipped++)) || true
            continue
        fi
        patch_entries+=("{\"name\":\"${name}\",\"value\":\"${value}\"}")
        echo "  SET:  ${name}=${value}"
        ((found++)) || true
    done

    if [[ ${found} -eq 0 ]]; then
        echo ""
        echo "No RELATED_IMAGE_* env vars to inject (${skipped} skipped)."
        echo "Ensure opt/manifests/ and opt/charts/ are populated (run get_all_manifests.sh)."
        return
    fi

    local env_json
    env_json=$(printf '%s\n' "${patch_entries[@]}" | paste -sd',' -)

    local container_idx
    container_idx=$(${KUBECTL} get deployment "${DEPLOY_NAME}" -n "${NS}" \
        -o jsonpath='{range .spec.template.spec.containers[*]}{.name}{"\n"}{end}' \
        | grep -n 'manager' | cut -d: -f1 || echo "1")
    container_idx=$((container_idx - 1))

    local patch="[{\"op\":\"add\",\"path\":\"/spec/template/spec/containers/${container_idx}/env\",\"value\":[${env_json}]}]"

    echo ""
    echo "Patching ${NS}/${DEPLOY_NAME} with ${found} RELATED_IMAGE_* env vars..."
    ${KUBECTL} patch deployment "${DEPLOY_NAME}" -n "${NS}" --type=json -p "${patch}"

    echo ""
    echo "Operator pod will restart with injected images."
    echo "  Found: ${found}  Skipped: ${skipped}"
}

main() {
    echo "=== inject-dev-images ==="
    echo ""

    if [[ ! -d "${MANIFESTS_ROOT}" && ! -d "${CHARTS_ROOT}" ]]; then
        echo "ERROR: Neither opt/manifests/ nor opt/charts/ found." >&2
        echo "Run 'get_all_manifests.sh' first." >&2
        exit 1
    fi

    detect_operator

    echo ""
    echo "Collecting RELATED_IMAGE_* env var names from Go source..."
    mapfile -t env_names < <(collect_image_env_names)
    echo "  Found ${#env_names[@]} env var names in source."

    echo ""
    echo "Extracting image references from opt/ artifacts..."
    declare -A artifact_images
    collect_images_from_artifacts artifact_images
    echo "  Found ${#artifact_images[@]} images in artifacts."

    echo ""
    echo "Building patch..."
    build_and_apply_patch env_names artifact_images
}

main "$@"
