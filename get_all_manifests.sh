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
    ["dashboard"]="opendatahub-io:odh-dashboard:main@b46b6a5d4f6f2c46c539530027a58cd6974efc3e:manifests"
    ["workbenches/kf-notebook-controller"]="opendatahub-io:kubeflow:main@0fc24b48fad6af13d64ff6bf930d101964b5c588:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="opendatahub-io:kubeflow:main@0fc24b48fad6af13d64ff6bf930d101964b5c588:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="opendatahub-io:notebooks:main@1266e9a942ac899c8a7c42ea4a4f4d8e73775461:manifests"
    ["kserve"]="opendatahub-io:kserve:release-v0.15@379909884399e0cdbb720d584219e6c7b8d11fdd:config"
    ["ray"]="opendatahub-io:kuberay:dev@b5ee4c9963783dad6a8917abfa29c4e91d8630ec:ray-operator/config"
    ["trustyai"]="opendatahub-io:trustyai-service-operator:incubation@25e144dd0f4c311c53cbe069fed18fb93fdb3a7d:config"
    ["modelregistry"]="opendatahub-io:model-registry-operator:main@279acc6ea21f1c0ad11c1deaabf48819bb868f03:config"
    ["trainingoperator"]="opendatahub-io:training-operator:stable@bfceb07994dc72751c4f445f8d27e673292bc0b5:manifests"
    ["datasciencepipelines"]="opendatahub-io:data-science-pipelines-operator:main@976f622093eeab657c36c12215d305faeb1d879a:config"
    ["modelcontroller"]="opendatahub-io:odh-model-controller:incubating@9a7657e99fb5cb6ab0c80861b621dcc3210ae029:config"
    ["feastoperator"]="opendatahub-io:feast:stable@0c85e244d3182b4570164f6d66c8b106a207c070:infra/feast-operator/config"
    ["llamastackoperator"]="opendatahub-io:llama-stack-k8s-operator:odh@76895fce0f00a3a7e147b7f5689a7d1b4ed5b6c9:config"
    ["trainer"]="opendatahub-io:trainer:stable@6b4be8aa3c4a12cf81b94bba4aca5d9b3e52d906:manifests"
    ["maas"]="opendatahub-io:maas-billing:main@6bcdb80286dd20bcac27ec0aaa06c1c1828e3327:deployment"
    ["mlflowoperator"]="opendatahub-io:mlflow-operator:main@08fd16ae240c0c8118741c4e7fdc34541f9965e3:config"
    ["sparkoperator"]="opendatahub-io:spark-operator:main@ca38ec94e2a7859d65d6c8b045181ab6b7dd8e51:config"
)

# RHOAI Component Manifests
declare -A RHOAI_COMPONENT_MANIFESTS=(
    ["dashboard"]="red-hat-data-services:odh-dashboard:rhoai-3.4:manifests"
    ["workbenches/kf-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-3.4:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-3.4:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="red-hat-data-services:notebooks:rhoai-3.4:manifests"
    ["kserve"]="red-hat-data-services:kserve:rhoai-3.4:config"
    ["ray"]="red-hat-data-services:kuberay:rhoai-3.4:ray-operator/config"
    ["trustyai"]="red-hat-data-services:trustyai-service-operator:rhoai-3.4:config"
    ["modelregistry"]="red-hat-data-services:model-registry-operator:rhoai-3.4:config"
    ["trainingoperator"]="red-hat-data-services:training-operator:rhoai-3.4:manifests"
    ["datasciencepipelines"]="red-hat-data-services:data-science-pipelines-operator:rhoai-3.4:config"
    ["modelcontroller"]="red-hat-data-services:odh-model-controller:rhoai-3.4:config"
    ["feastoperator"]="red-hat-data-services:feast:rhoai-3.4:infra/feast-operator/config"
    ["llamastackoperator"]="red-hat-data-services:llama-stack-k8s-operator:rhoai-3.4:config"
    ["trainer"]="red-hat-data-services:trainer:rhoai-3.4:manifests"
    ["maas"]="red-hat-data-services:maas-billing:rhoai-3.4:deployment"
    ["mlflowoperator"]="red-hat-data-services:mlflow-operator:rhoai-3.4:config"
    ["sparkoperator"]="red-hat-data-services:spark-operator:rhoai-3.4:config"
)

# {ODH,RHOAI}_COMPONENT_CHARTS are lists of chart repositories info to fetch helm charts
# in the same format as manifests: "repo-org:repo-name:ref-name:source-folder"
# key is the target folder under charts/

# ODH Component Charts
declare -A ODH_COMPONENT_CHARTS=(
    ["cert-manager-operator"]="opendatahub-io:odh-gitops:main@99d7a56589872aea722797e90f12b8801aad1065:charts/cert-manager-operator"
    ["lws-operator"]="opendatahub-io:odh-gitops:main@99d7a56589872aea722797e90f12b8801aad1065:charts/lws-operator"
    ["sail-operator"]="opendatahub-io:odh-gitops:main@99d7a56589872aea722797e90f12b8801aad1065:charts/sail-operator"
)

# RHOAI Component Charts
declare -A RHOAI_COMPONENT_CHARTS=(
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

    if [[ -v USE_LOCAL ]] && [[ -e ../${repo_name} ]]; then
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
