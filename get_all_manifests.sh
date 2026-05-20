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
    ["dashboard"]="opendatahub-io:odh-dashboard:main@f367034037e76c7b77156093d5563067baa2abe3:manifests"
    ["workbenches/kf-notebook-controller"]="opendatahub-io:kubeflow:main@f09b56e860ff88bcc05668b3f517791cdccd5b4d:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="opendatahub-io:kubeflow:main@f09b56e860ff88bcc05668b3f517791cdccd5b4d:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="opendatahub-io:notebooks:main@b3803ad4e49d09613f01f8362d5455fd70da204b:manifests"
    ["kserve"]="opendatahub-io:kserve:release-v0.17@5807eceb6db0de0e865fcedbb84ffe06ff9224e1:config"
    ["ray"]="opendatahub-io:kuberay:dev@a05db5b9c89f67087d1ecbf091f56c8a4599d689:ray-operator/config"
    ["trustyai"]="opendatahub-io:trustyai-service-operator:incubation@de96668b0690db47574bab3ff737e5748be235ee:config"
    ["modelregistry"]="opendatahub-io:model-registry-operator:main@a982cf87b95fb3054aa4333a6bedb6df1bff1616:config"
    ["trainingoperator"]="opendatahub-io:training-operator:stable@28a60bd79b9dbbb39cd674d3660fa27ab1b42bdb:manifests"
    ["datasciencepipelines"]="opendatahub-io:data-science-pipelines-operator:main@ba2d887a412d31e2f0afcebfad7fc71de3ac6521:config"
    ["modelcontroller"]="opendatahub-io:odh-model-controller:incubating@3632f68dc8c0b2be5b473d50f6e87a8f9268e344:config"
    ["feastoperator"]="opendatahub-io:feast:stable@fc14c10d9ef18d94f311036840eefb4ed265575d:infra/feast-operator/config"
    ["ogx"]="opendatahub-io:ogx-k8s-operator:odh@54ce7ea2e3501040c33c1d1b5ab9a69ef51ceadf:config"
    ["trainer"]="opendatahub-io:trainer:stable@51baadf644cd5d2c1672f1c658be46ad82f01b44:manifests"
    ["maas"]="opendatahub-io:models-as-a-service:stable@322a2753643fefe47920d22a41fadeb4587993da:deployment"
    ["mlflowoperator"]="opendatahub-io:mlflow-operator:main@4cccfcc2dd8576cabbf255f66894d801a68eb844:config"
    ["sparkoperator"]="opendatahub-io:spark-operator:main@bc7885e2d34a9a0293672c1e8155e5446dcc0722:config"
    ["wva"]="opendatahub-io:workload-variant-autoscaler:main@0641ee03e8430cce5821797137b1b53bf70b98a2:config"
)

# RHOAI Component Manifests
declare -A RHOAI_COMPONENT_MANIFESTS=(
    ["dashboard"]="red-hat-data-services:odh-dashboard:rhoai-3.5-ea.1@dae645de59aaf8ce52a87ebcc257c3a657c370ec:manifests"
    ["workbenches/kf-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-3.5-ea.1@a0e3fd64885304ad5aa972af576bb78f76adf9ec:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-3.5-ea.1@a0e3fd64885304ad5aa972af576bb78f76adf9ec:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="red-hat-data-services:notebooks:rhoai-3.5-ea.1@2fd3c903d22110d8199c1d4683209c4e092f0b57:manifests"
    ["kserve"]="red-hat-data-services:kserve:rhoai-3.5-ea.1@610006b8241d2aa088681fd3c731ed0c9365374b:config"
    ["ray"]="red-hat-data-services:kuberay:rhoai-3.5-ea.1@eb4a3f1fe24f0da81ca56b290ad6055e61c23e12:ray-operator/config"
    ["trustyai"]="red-hat-data-services:trustyai-service-operator:rhoai-3.5-ea.1@ea5e370d575631b78e20c9b8f5404639685b3f2e:config"
    ["modelregistry"]="red-hat-data-services:model-registry-operator:rhoai-3.5-ea.1@07fe57418877d2470ae8b79c5ea1011de8511798:config"
    ["trainingoperator"]="red-hat-data-services:training-operator:rhoai-3.5-ea.1@3b80823712edc8a8a43c50edc68a5b0c662734dc:manifests"
    ["datasciencepipelines"]="red-hat-data-services:data-science-pipelines-operator:rhoai-3.5-ea.1@c4e9d9d5198f9f7a4ff0ab00d64bad3293f93e4d:config"
    ["modelcontroller"]="red-hat-data-services:odh-model-controller:rhoai-3.5-ea.1@ca183a5a23edb1dbb727ff30cf681c20691c0e6b:config"
    ["feastoperator"]="red-hat-data-services:feast:rhoai-3.5-ea.1@13314edeb99dba402c6b6bc4e1ff54c458da2d27:infra/feast-operator/config"
    ["ogx"]="red-hat-data-services:ogx-k8s-operator:rhoai-3.5-ea.1@bf732614a7e78c5477422d137360d2f1b3a895cf:config"
    ["trainer"]="red-hat-data-services:trainer:rhoai-3.5-ea.1@138f5c9d590be0e7aa548798b20ae1b2fa5ac6b9:manifests"
    ["maas"]="red-hat-data-services:maas-billing:rhoai-3.5-ea.1@043ad962a27b3d56d37ef78236f77ec0a5cd1017:deployment"
    ["mlflowoperator"]="red-hat-data-services:mlflow-operator:rhoai-3.5-ea.1@d35cc1f47f802042a24f57a669f7f7e6f41763f8:config"
    ["sparkoperator"]="red-hat-data-services:spark-operator:rhoai-3.5-ea.1@60c9844460b01d3795baf49197877aa1e2006813:config"
    ["wva"]="red-hat-data-services:workload-variant-autoscaler:rhoai-3.5-ea.1@418960bad81e4d3e74dff64d07692bc8b5ca51e4:config"
)

# {ODH,RHOAI}_COMPONENT_CHARTS are lists of chart repositories info to fetch helm charts
# in the same format as manifests: "repo-org:repo-name:ref-name:source-folder"
# key is the target folder under charts/

# ODH Component Charts
declare -A ODH_COMPONENT_CHARTS=(
    ["cert-manager-operator"]="opendatahub-io:odh-gitops:main@5beea1535c84b36730587de3d2551c431d62d1bd:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="opendatahub-io:odh-gitops:main@5beea1535c84b36730587de3d2551c431d62d1bd:charts/dependencies/lws-operator"
    ["sail-operator"]="opendatahub-io:odh-gitops:main@5beea1535c84b36730587de3d2551c431d62d1bd:charts/dependencies/sail-operator"
    ["gateway-api"]="opendatahub-io:odh-gitops:main@5beea1535c84b36730587de3d2551c431d62d1bd:charts/dependencies/gateway-api"
)

# RHOAI Component Charts
declare -A RHOAI_COMPONENT_CHARTS=(
    ["cert-manager-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5-ea.1@2f6a53ff4b6dc968cd8fc482ecd53552874c977b:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5-ea.1@2f6a53ff4b6dc968cd8fc482ecd53552874c977b:charts/dependencies/lws-operator"
    ["sail-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5-ea.1@2f6a53ff4b6dc968cd8fc482ecd53552874c977b:charts/dependencies/sail-operator"
    ["gateway-api"]="red-hat-data-services:odh-gitops:rhoai-3.5-ea.1@2f6a53ff4b6dc968cd8fc482ecd53552874c977b:charts/dependencies/gateway-api"
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
