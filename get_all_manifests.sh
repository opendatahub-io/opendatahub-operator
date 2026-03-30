#!/usr/bin/env bash
set -e

GITHUB_URL="https://github.com"
DST_MANIFESTS_DIR="./opt/manifests"
DST_CHARTS_DIR="./opt/charts"

# {ODH,RHOAI}_COMPONENT_MANIFESTS are lists of components repositories info to fetch the manifests
# in the format of "repo-org:repo-name:ref-name:source-folder" and key is the target folder under manifests/
# ref-name can be a branch name, tag name, or a commit SHA (7-40 hex characters)
# ref-name supports:
# 1. "branch" - tracks latest commit on branch (e.g., main)
# 2. "tag" - immutable reference (e.g., v1.0.0)
# 3. "branch@commit-sha" - tracks branch but pinned to specific commit (e.g., main@a1b2c3d4)

# ODH Component Manifests
declare -A ODH_COMPONENT_MANIFESTS=(
    ["dashboard"]="opendatahub-io:odh-dashboard:main@5ef79994526933c903ce51056e8c75d51074f416:manifests"
    ["workbenches/kf-notebook-controller"]="opendatahub-io:kubeflow:main@92ea7b53c6101aefdb109d36e7cd3129d9ad5ced:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="opendatahub-io:kubeflow:main@92ea7b53c6101aefdb109d36e7cd3129d9ad5ced:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="opendatahub-io:notebooks:main@e10dd069bfb12c4cab7ac83c1d3f89f51c0f44db:manifests"
    ["kserve"]="opendatahub-io:kserve:release-v0.17@a3c6be0b5d5c28b6196cf2b354b58ef3d25dc926:config"
    ["ray"]="opendatahub-io:kuberay:dev@0384218c547fd988a8427bbe15247a45d04b794c:ray-operator/config"
    ["trustyai"]="opendatahub-io:trustyai-service-operator:incubation@c6b2f1cd3a78fd2e8f71536614768ba099ce8b26:config"
    ["modelregistry"]="opendatahub-io:model-registry-operator:main@fe7d327a2c08978c611062a6b36ec6aaf9426287:config"
    ["trainingoperator"]="opendatahub-io:training-operator:stable@28a60bd79b9dbbb39cd674d3660fa27ab1b42bdb:manifests"
    ["datasciencepipelines"]="opendatahub-io:data-science-pipelines-operator:main@2817bdf9613754dac1961dffa738007de3b398da:config"
    ["modelcontroller"]="opendatahub-io:odh-model-controller:incubating@0c3ee6142e9310cf4015a3159c848f0c24225547:config"
    ["feastoperator"]="opendatahub-io:feast:stable@d7733ac6ab59274f07c8d583e48f1b08636f8997:infra/feast-operator/config"
    ["llamastackoperator"]="opendatahub-io:llama-stack-k8s-operator:odh@ba8020a4fc5b6ac86e14aea251992ee2ccdde5ef:config"
    ["trainer"]="opendatahub-io:trainer:stable@916e0dffe43b92618d068e9f138fd894294c1456:manifests"
    ["maas"]="opendatahub-io:models-as-a-service:stable@28c456c0b33180eb3d385572f95af420feaa5513:deployment"
    ["mlflowoperator"]="opendatahub-io:mlflow-operator:main@e177c34a5b413ec5e2ba530e0cde199074138d8c:config"
    ["sparkoperator"]="opendatahub-io:spark-operator:main@924ba83b1c4a963748d5e192fbec09a4340990cd:config"
    ["wva"]="opendatahub-io:workload-variant-autoscaler:main@7cb1465b00cda0b800c3b252dd6f2a1cfcd2453a:config"
)

# RHOAI Component Manifests
RHOAI_BRANCH="${RHOAI_BRANCH:-main}"
declare -A RHOAI_COMPONENT_MANIFESTS=(
    ["dashboard"]="red-hat-data-services:odh-dashboard:rhoai-3.4@658e2eba40786f213882162351cdddc7be7a55da:manifests"
    ["workbenches/kf-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-3.4@41652af3581ecf948e3bce05aca19d90d3eb0ad8:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-3.4@41652af3581ecf948e3bce05aca19d90d3eb0ad8:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="red-hat-data-services:notebooks:rhoai-3.4@ebddf988d9f285248a3a028260031b841b52431d:manifests"
    ["kserve"]="red-hat-data-services:kserve:rhoai-3.4@90edb52c67a466cf1478590463aab96073d873bb:config"
    ["ray"]="red-hat-data-services:kuberay:rhoai-3.4@b825f4adfc0a1cb122ce320228baceace2f1a2fa:ray-operator/config"
    ["trustyai"]="red-hat-data-services:trustyai-service-operator:rhoai-3.4@2e913a65396fc67424e05fa4ccb03da608242eac:config"
    ["modelregistry"]="red-hat-data-services:model-registry-operator:rhoai-3.4@015062c882a964d488f7c3151fac9a30b31623f5:config"
    ["trainingoperator"]="red-hat-data-services:training-operator:rhoai-3.4@8d2e0bf502fcffa22545201339baf5bce96c8a63:manifests"
    ["datasciencepipelines"]="red-hat-data-services:data-science-pipelines-operator:rhoai-3.4@b90290f65f67cb508a12d0e579e35776d89a4d48:config"
    ["modelcontroller"]="red-hat-data-services:odh-model-controller:rhoai-3.4@421e05bd40489d010329946e9924bcd273209489:config"
    ["feastoperator"]="red-hat-data-services:feast:rhoai-3.4@cfb33e6a5b5b77b82ae9dc7a0e4844c64186559b:infra/feast-operator/config"
    ["llamastackoperator"]="red-hat-data-services:llama-stack-k8s-operator:rhoai-3.4@8cc0a4f19368988eeaf34702b2ddf1c771505661:config"
    ["trainer"]="red-hat-data-services:trainer:rhoai-3.4@f5d41a3679067f707ab62e0ba442fd677903eb6a:manifests"
    ["maas"]="red-hat-data-services:maas-billing:rhoai-3.4@c04f0e2b68f545fbe2deca2ca9b19972e3baac0b:deployment"
    ["mlflowoperator"]="red-hat-data-services:mlflow-operator:rhoai-3.4@45ad3d560ce1e241733e55614d157f53c7cbeacc:config"
    ["sparkoperator"]="red-hat-data-services:spark-operator:rhoai-3.4@8ecee53b4752854eeff7456ddaf9a81251147ca4:config"
    ["wva"]="red-hat-data-services:workload-variant-autoscaler:rhoai-3.4@63b6911bb789e5fbc800e526cea2cc5168a2543c:config"
)

# {ODH,RHOAI}_COMPONENT_CHARTS are lists of chart repositories info to fetch helm charts
# in the same format as manifests: "repo-org:repo-name:ref-name:source-folder"
# key is the target folder under charts/

# ODH Component Charts
declare -A ODH_COMPONENT_CHARTS=(
    ["cert-manager-operator"]="opendatahub-io:odh-gitops:main@cb5b32f982839b1f649dfb9d5d4e1f3cb79e669a:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="opendatahub-io:odh-gitops:main@cb5b32f982839b1f649dfb9d5d4e1f3cb79e669a:charts/dependencies/lws-operator"
    ["sail-operator"]="opendatahub-io:odh-gitops:main@cb5b32f982839b1f649dfb9d5d4e1f3cb79e669a:charts/dependencies/sail-operator"
    ["gateway-api"]="opendatahub-io:odh-gitops:main@cb5b32f982839b1f649dfb9d5d4e1f3cb79e669a:charts/dependencies/gateway-api"
)

# RHOAI Component Charts
declare -A RHOAI_COMPONENT_CHARTS=(
    ["cert-manager-operator"]="red-hat-data-services:odh-gitops:rhoai-3.4@eaeef9830e88ff9a6f588d4b1cb38efd3cb54cc2:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="red-hat-data-services:odh-gitops:rhoai-3.4@eaeef9830e88ff9a6f588d4b1cb38efd3cb54cc2:charts/dependencies/lws-operator"
    ["sail-operator"]="red-hat-data-services:odh-gitops:rhoai-3.4@eaeef9830e88ff9a6f588d4b1cb38efd3cb54cc2:charts/dependencies/sail-operator"
    ["gateway-api"]="red-hat-data-services:odh-gitops:rhoai-3.4@eaeef9830e88ff9a6f588d4b1cb38efd3cb54cc2:charts/dependencies/gateway-api"
)

# Select the appropriate manifest based on platform type
if [ "${ODH_PLATFORM_TYPE:-OpenDataHub}" = "OpenDataHub" ]; then
    echo "Cloning manifests and charts for ODH"
    declare -A COMPONENT_MANIFESTS=()
    for key in "${!ODH_COMPONENT_MANIFESTS[@]}"; do
        COMPONENT_MANIFESTS["$key"]="${ODH_COMPONENT_MANIFESTS[$key]}"
    done
    declare -A COMPONENT_CHARTS=()
    for key in "${!ODH_COMPONENT_CHARTS[@]}"; do
        COMPONENT_CHARTS["$key"]="${ODH_COMPONENT_CHARTS[$key]}"
    done
else
    echo "Cloning manifests and charts for RHOAI"
    declare -A COMPONENT_MANIFESTS=()
    for key in "${!RHOAI_COMPONENT_MANIFESTS[@]}"; do
        COMPONENT_MANIFESTS["$key"]="${RHOAI_COMPONENT_MANIFESTS[$key]}"
    done
    declare -A COMPONENT_CHARTS=()
    for key in "${!RHOAI_COMPONENT_CHARTS[@]}"; do
        COMPONENT_CHARTS["$key"]="${RHOAI_COMPONENT_CHARTS[$key]}"
    done
fi

# PLATFORM_MANIFESTS is a list of manifests that are contained in the operator repository. Please also add them to the
# Dockerfile COPY instructions. Declaring them here causes this script to create a symlink in the manifests folder, so
# they can be easily modified during development, but during a container build, they must be copied into the proper
# location instead, as this script DOES NOT manage platform manifest files for a container build.
declare -A PLATFORM_MANIFESTS=(
    ["osd-configs"]="config/osd-configs"
    ["monitoring"]="config/monitoring"
    ["hardwareprofiles"]="config/hardwareprofiles"
    ["connectionAPI"]="config/connectionAPI"
)

# Allow overwriting repo using flags component=repo
# Updated pattern to accept commit SHAs (7-40 hex chars) and branch@sha format in addition to branches/tags
pattern="^[a-zA-Z0-9_.-]+:[a-zA-Z0-9_.-]+:([a-zA-Z0-9_./-]+|[a-zA-Z0-9_./-]+@[a-f0-9]{7,40}):[a-zA-Z0-9_./-]+$"
if [ "$#" -ge 1 ]; then
    for arg in "$@"; do
        if [[ $arg == --* ]]; then
            arg="${arg:2}"  # Remove the '--' prefix
            IFS="=" read -r key value <<< "$arg"
            if [[ -n "${COMPONENT_MANIFESTS[$key]}" ]]; then
                if [[ ! $value =~ $pattern ]]; then
                    echo "ERROR: The value '$value' does not match the expected format 'repo-org:repo-name:ref-name:source-folder'."
                    continue
                fi
                COMPONENT_MANIFESTS["$key"]=$value
            else
                echo "ERROR: '$key' does not exist in COMPONENT_MANIFESTS, it will be skipped."
                echo "Available components are: [${!COMPONENT_MANIFESTS[@]}]"
                exit 1
            fi
        else
            echo "Warning: Argument '$arg' does not follow the '--key=value' format."
        fi
    done
fi

TMP_DIR=$(mktemp -d -t "odh-manifests.XXXXXXXXXX")
trap '{ rm -rf -- "$TMP_DIR"; }' EXIT

function try_fetch_ref()
{
    local repo=$1
    local ref_type=$2  # "tags" or "heads"
    local ref=$3

    local git_ref="refs/$ref_type/$ref"
    local ref_name=$([[ $ref_type == "tags" ]] && echo "tag" || echo "branch")

    if git ls-remote --exit-code "$repo" "$git_ref" &>/dev/null; then
        if git fetch -q --depth 1 "$repo" "$git_ref" && git reset -q --hard FETCH_HEAD; then
            return 0
        else
            echo "ERROR: Failed to fetch $ref_name $ref from $repo"
            return 1
        fi
    fi
    return 1
}

function git_fetch_ref()
{
    local repo=$1
    local ref=$2
    local dir=$3

    mkdir -p $dir
    pushd $dir &>/dev/null
    git init -q

    # Check if ref is in tracking format: branch@sha
    if [[ $ref =~ ^([a-zA-Z0-9_./-]+)@([a-f0-9]{7,40})$ ]]; then
        local commit_sha="${BASH_REMATCH[2]}"

        # For tracking format, fetch the specific commit SHA
        git remote add origin $repo
        if ! git fetch --depth 1 -q origin $commit_sha; then
            echo "ERROR: Failed to fetch from repository $repo"
            popd &>/dev/null
            return 1
        fi
        if ! git reset -q --hard $commit_sha 2>/dev/null; then
            echo "ERROR: Commit SHA $commit_sha not found in repository $repo"
            popd &>/dev/null
            return 1
        fi
    else
        # Original logic for branches, tags, and plain commit SHAs
        # Try to fetch as tag first, then as branch
        if try_fetch_ref "$repo" "tags" "$ref" || try_fetch_ref "$repo" "heads" "$ref"; then
            # Successfully fetched tag or branch
            :  # no-op, we're done
        else
            echo "ERROR: '$ref' is not a valid branch, tag, or commit SHA in repository $repo"
            echo "You can check available refs with:"
            echo "  git ls-remote --heads $repo  # for branches"
            echo "  git ls-remote --tags $repo   # for tags"
            popd &>/dev/null
            return 1
        fi
    fi

    popd &>/dev/null
}

download_repo_content() {
    local key=$1
    local repo_info=$2
    local dst_dir=$3
    echo -e "\033[32mCloning repo \033[33m${key}\033[32m:\033[0m ${repo_info}"
    IFS=':' read -r -a repo_info <<< "${repo_info}"

    repo_org="${repo_info[0]}"
    repo_name="${repo_info[1]}"
    repo_ref="${repo_info[2]}"
    source_path="${repo_info[3]}"
    target_path="${key}"

    repo_url="${GITHUB_URL}/${repo_org}/${repo_name}"
    repo_dir="${TMP_DIR}/${dst_dir}/${key}"

    if [[ "${USE_LOCAL}" == "true" ]] && [[ -e ../${repo_name} ]]; then
        echo "copying from adjacent checkout ..."
        mkdir -p "${dst_dir}/${target_path}"
        cp -rf "../${repo_name}/${source_path}"/* "${dst_dir}/${target_path}"
        return
    fi

    if ! git_fetch_ref ${repo_url} ${repo_ref} ${repo_dir}; then
        echo "ERROR: Failed to fetch ref '${repo_ref}' from '${repo_url}' for component '${key}'"
        return 1
    fi

    mkdir -p "${dst_dir}/${target_path}"
    cp -rf "${repo_dir}/${source_path}"/* "${dst_dir}/${target_path}"
}

download_manifest() {
    download_repo_content "$1" "$2" "${DST_MANIFESTS_DIR}"
}

download_chart() {
    download_repo_content "$1" "$2" "${DST_CHARTS_DIR}"
}

# Track background job PIDs +declare -a pids=()
# Use parallel processing
for key in "${!COMPONENT_MANIFESTS[@]}"; do
    download_manifest "$key" "${COMPONENT_MANIFESTS[$key]}" &
    pids+=($!)
done
# Wait and check exit codes
failed=0
for pid in "${pids[@]}"; do
    if ! wait "$pid"; then
        failed=1
    fi
done
if [ $failed -eq 1 ]; then
    echo "One or more downloads failed"
    exit 1
fi

# Download charts in parallel
if [ ${#COMPONENT_CHARTS[@]} -gt 0 ]; then
    declare -a chart_pids=()
    for key in "${!COMPONENT_CHARTS[@]}"; do
        download_chart "$key" "${COMPONENT_CHARTS[$key]}" &
        chart_pids+=($!)
    done
    for pid in "${chart_pids[@]}"; do
        if ! wait "$pid"; then
            failed=1
        fi
    done
    if [ $failed -eq 1 ]; then
        echo "One or more chart downloads failed"
        exit 1
    fi
fi

for key in "${!PLATFORM_MANIFESTS[@]}"; do
    source_path="${PLATFORM_MANIFESTS[$key]}"
    target_path="${key}"

    if [[ -d ${source_path} && ! -L ${DST_MANIFESTS_DIR}/${target_path} ]]; then
        echo -e "\033[32mSymlinking local manifest \033[33m${key}\033[32m:\033[0m ${source_path}"
        ln -s $(pwd)/${source_path} ${DST_MANIFESTS_DIR}/${target_path}
    fi
done
