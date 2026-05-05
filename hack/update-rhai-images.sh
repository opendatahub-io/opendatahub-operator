#!/usr/bin/env bash
# Updates RHAI component manifests with pinned images from the
# RHOAI-Build-Config CSV (ClusterServiceVersion).
#
# Usage: ./hack/update-rhai-images.sh [--branch <branch>] [-c <component>]
#
# Examples:
#   RHAI_BRANCH=rhoai-3.5 make update-rhai-images
#   RHAI_BRANCH=rhoai-3.5 ./hack/update-rhai-images.sh
#   ./hack/update-rhai-images.sh --branch rhoai-3.5
#   ./hack/update-rhai-images.sh --branch rhoai-3.5 -c kserve
#   ./hack/update-rhai-images.sh --branch rhoai-3.5 -c kserve,modelcontroller

set -euo pipefail

BUILD_CONFIG_REPO="https://github.com/red-hat-data-services/RHOAI-Build-Config"
CSV_PATH="bundle/manifests/rhods-operator.clusterserviceversion.yaml"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MANIFESTS_DIR="${MANIFESTS_DIR:-${SCRIPT_DIR}/opt/manifests}"
[[ -d "$MANIFESTS_DIR" ]] || { echo "ERROR: Manifests directory not found: ${MANIFESTS_DIR}"; exit 1; }
COMPONENTS_DIR="${SCRIPT_DIR}/internal/controller/components"

YQ="${YQ:-$(command -v yq || true)}"
[[ -n "$YQ" ]] || { echo "ERROR: yq is required but not found in PATH"; exit 1; }

SED_COMMAND="${SED_COMMAND:-sed}"

# This is a list we need maintain esp. when new component is added
declare -A RHAI_COMPONENT_PATHS=(
    ["dashboard"]="dashboard/rhoai/onprem dashboard/modular-architecture"
    ["datasciencepipelines"]="datasciencepipelines/base"
    ["feastoperator"]="feastoperator/overlays/rhoai"
    ["kserve"]="kserve/overlays/odh"
    ["llamastackoperator"]="llamastackoperator/overlays/rhoai"
    ["mlflowoperator"]="mlflowoperator/base"
    ["modelcontroller"]="modelcontroller/base wva/openshift"
    ["modelregistry"]="modelregistry/overlays/odh"
    ["modelsasservice"]="maas/overlays/odh"
    ["ray"]="ray/openshift"
    ["sparkoperator"]="sparkoperator/overlays/rhoai"
    ["trainer"]="trainer/rhoai"
    ["trainingoperator"]="trainingoperator/rhoai"
    ["trustyai"]="trustyai/overlays/rhoai trustyai/overlays/mcp-guardrails"
    ["workbenches"]="workbenches/odh-notebook-controller/base workbenches/kf-notebook-controller/overlays/openshift workbenches/notebooks/rhoai/base:params-latest.env"
)

COMPONENT_FILTER=""
while [[ $# -gt 0 ]]; do
    case $1 in
        --branch)
            [[ $# -ge 2 ]] || { echo "ERROR: --branch requires a value"; exit 1; }
            RHAI_BRANCH="$2"; shift 2;;
        -c|--component)
            [[ $# -ge 2 ]] || { echo "ERROR: -c/--component requires a value"; exit 1; }
            COMPONENT_FILTER="$2"; shift 2;;
        *)  echo "ERROR: Unknown argument: $1"; exit 1;;
    esac
done
if [[ -z "${RHAI_BRANCH:-}" ]]; then
    echo "ERROR: RHAI_BRANCH is not set. Use --branch flag or set RHAI_BRANCH env var."
    exit 1
fi

echo "RHAI branch: ${RHAI_BRANCH}"
echo "Manifests dir: ${MANIFESTS_DIR}"
if [[ -n "$COMPONENT_FILTER" ]]; then
    echo "Component filter: ${COMPONENT_FILTER}"
fi

TMP_DIR=$(mktemp -d -t "rhai-images.XXXXXXXXXX")
trap 'rm -rf "$TMP_DIR"' EXIT

if ! git clone --depth 1 -b "${RHAI_BRANCH}" -q "${BUILD_CONFIG_REPO}" "${TMP_DIR}/build-config"; then
    echo "ERROR: Failed to clone ${BUILD_CONFIG_REPO} branch ${RHAI_BRANCH}"
    exit 1
fi

CSV_FILE="${TMP_DIR}/build-config/${CSV_PATH}"
if [[ ! -f "$CSV_FILE" ]]; then
    echo "ERROR: ${CSV_PATH} not found in repo"
    exit 1
fi

# get RELATED_IMAGE_* env vars from the CSV
declare -A CSV_IMAGES
while IFS='=' read -r name value; do
    [[ -n "$name" ]] && CSV_IMAGES["$name"]="$value"
done < <("${YQ}" eval '
    .spec.install.spec.deployments[].spec.template.spec.containers[].env[]
    | select(.name == "RELATED_IMAGE_*")
    | .name + "=" + .value
' "$CSV_FILE" | tr -d '"')

echo "Found ${#CSV_IMAGES[@]} RELATED_IMAGE entries in CSV"

updated=0; skipped=0; unmapped=0
SKIPPED_LIST=()
UNMAPPED_LIST=()

update_params_env() {
    local file="${MANIFESTS_DIR}/$1" key="$2" value="$3"
    if [[ ! -f "$file" ]]; then
        return 1
    fi
    if ! grep -q "^${key}=" "$file"; then
        return 1
    fi
    $SED_COMMAND -i'' "s#^${key}=.*#${key}=${value}#" "$file"
}

declare -A IMAGE_MAP
build_image_map() {
    local comp_dirs=()

    if [[ -n "$COMPONENT_FILTER" ]]; then
        IFS=',' read -ra filter_list <<< "$COMPONENT_FILTER"
        for comp in "${filter_list[@]}"; do
            comp=$(echo "$comp" | xargs)
            if [[ ! -d "${COMPONENTS_DIR}/${comp}" ]]; then
                echo "ERROR: Component directory not found: ${COMPONENTS_DIR}/${comp}" >&2
                exit 1
            fi
            comp_dirs+=("$comp")
        done
    else
        for d in "${COMPONENTS_DIR}"/*/; do
            [[ -d "$d" ]] || continue
            local name
            name=$(basename "$d")
            [[ "$name" == "registry" ]] && continue
            [[ -z "${RHAI_COMPONENT_PATHS[$name]:-}" ]] && continue
            comp_dirs+=("$name")
        done
    fi

    for comp in "${comp_dirs[@]}"; do
        local rhai_paths="${RHAI_COMPONENT_PATHS[$comp]:-}"
        if [[ -z "$rhai_paths" ]]; then
            continue
        fi
        grep -rn '"[^"]*"[[:space:]]*:[[:space:]]*"RELATED_IMAGE_[A-Z0-9_]*"' \
            "${COMPONENTS_DIR}/${comp}" \
            --include='*.go' --exclude='*_test.go' 2>/dev/null | while IFS= read -r match; do
            local content key related_image
            content=$(echo "$match" | cut -d: -f3-)
            key=$(echo "$content" | sed 's/[[:space:]]*"\([^"]*\)"[[:space:]]*:[[:space:]]*"RELATED_IMAGE_[A-Z0-9_]*".*/\1/')
            related_image=$(echo "$content" | sed 's/.*:[[:space:]]*"\(RELATED_IMAGE_[A-Z0-9_]*\)".*/\1/')
            [[ -z "$key" || -z "$related_image" ]] && continue

            for path_entry in $rhai_paths; do
                local mpath pfile
                if [[ "$path_entry" == *:* ]]; then
                    mpath="${path_entry%%:*}"
                    pfile="${path_entry#*:}"
                else
                    mpath="$path_entry"
                    pfile="params.env"
                fi
                local full="${MANIFESTS_DIR}/${mpath}/${pfile}"
                if [[ -f "$full" ]] && grep -q "^${key}=" "$full"; then
                    echo "${related_image} ${mpath}/${pfile}:${key}"
                fi
            done
        done
    done
}

echo "Building image map from operator source..."
while read -r related_image target; do
    [[ -z "$related_image" ]] && continue
    if [[ -n "${IMAGE_MAP[$related_image]:-}" ]]; then
        if [[ " ${IMAGE_MAP[$related_image]} " != *" ${target} "* ]]; then
            IMAGE_MAP["$related_image"]+=" ${target}"
        fi
    else
        IMAGE_MAP["$related_image"]="${target}"
    fi
done < <(build_image_map)

declare -A HANDLED_IMAGES
for name in "${!IMAGE_MAP[@]}"; do
    HANDLED_IMAGES["$name"]=1
done

for related_name in "${!IMAGE_MAP[@]}"; do
    new_value="${CSV_IMAGES[$related_name]:-}"
    if [[ -z "$new_value" ]]; then
        SKIPPED_LIST+=("${related_name}")
        : $(( skipped += 1 ))
        continue
    fi
    for target in ${IMAGE_MAP[$related_name]}; do
        file="${target%%:*}"
        key="${target#*:}"
        if update_params_env "$file" "$key" "$new_value"; then
            : $(( updated += 1 ))
        else
            SKIPPED_LIST+=("${related_name} -> ${file}:${key}")
            : $(( skipped += 1 ))
        fi
    done
done

# unmapped: in CSV but not in any source code map
for related_name in "${!CSV_IMAGES[@]}"; do
    [[ -n "${HANDLED_IMAGES[$related_name]:-}" ]] && continue
    UNMAPPED_LIST+=("${related_name}")
    : $(( unmapped += 1 ))
done

echo "Updated: ${updated}  Not in CSV: ${skipped}  Not used: ${unmapped}"

if [[ ${#SKIPPED_LIST[@]} -gt 0 ]]; then
    echo ""
    echo "Not set in CSV:"
    for item in "${SKIPPED_LIST[@]}"; do
        echo "  - ${item}"
    done
fi

if [[ ${#UNMAPPED_LIST[@]} -gt 0 ]]; then
    echo ""
    echo "Listed in CSV but not replaced by Operator:"
    IFS=$'\n' sorted=($(sort <<<"${UNMAPPED_LIST[*]}")); unset IFS
    for item in "${sorted[@]}"; do
        echo "  - ${item}"
    done
fi
