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
    ["dashboard"]="opendatahub-io:odh-dashboard:main@1365e4ebb08a5353c8b90d6c5339054c88128630:manifests"
    ["workbenches/kf-notebook-controller"]="opendatahub-io:kubeflow:main@f09b56e860ff88bcc05668b3f517791cdccd5b4d:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="opendatahub-io:kubeflow:main@f09b56e860ff88bcc05668b3f517791cdccd5b4d:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="opendatahub-io:notebooks:main@a74bb4727765006f4875bec2d4c77f238018c26a:manifests"
    ["kserve"]="opendatahub-io:kserve:release-v0.17@5807eceb6db0de0e865fcedbb84ffe06ff9224e1:config"
    ["ray"]="opendatahub-io:kuberay:dev@8dc9afdcf3ace2a43949e3ee961f58594be1b986:ray-operator/config"
    ["trustyai"]="opendatahub-io:trustyai-service-operator:incubation@de96668b0690db47574bab3ff737e5748be235ee:config"
    ["modelregistry"]="opendatahub-io:model-registry-operator:main@a982cf87b95fb3054aa4333a6bedb6df1bff1616:config"
    ["trainingoperator"]="opendatahub-io:training-operator:stable@28a60bd79b9dbbb39cd674d3660fa27ab1b42bdb:manifests"
    ["datasciencepipelines"]="opendatahub-io:data-science-pipelines-operator:main@582a40651525aaae8f6a5f3b7bbcae4e75956453:config"
    ["modelcontroller"]="opendatahub-io:odh-model-controller:incubating@3632f68dc8c0b2be5b473d50f6e87a8f9268e344:config"
    ["feastoperator"]="opendatahub-io:feast:stable@fc14c10d9ef18d94f311036840eefb4ed265575d:infra/feast-operator/config"
    ["ogx"]="opendatahub-io:ogx-k8s-operator:odh@54ce7ea2e3501040c33c1d1b5ab9a69ef51ceadf:config"
    ["trainer"]="opendatahub-io:trainer:stable@51baadf644cd5d2c1672f1c658be46ad82f01b44:manifests"
    ["maas"]="opendatahub-io:models-as-a-service:stable@b409c456aeaf1437760a9d74a9594a453252f1f2:deployment"
    ["mlflowoperator"]="opendatahub-io:mlflow-operator:main@4cccfcc2dd8576cabbf255f66894d801a68eb844:config"
    ["sparkoperator"]="opendatahub-io:spark-operator:main@f91ff22af0b264e0cef090c2d0f1c94cae6b580e:config"
    ["wva"]="opendatahub-io:workload-variant-autoscaler:main@0641ee03e8430cce5821797137b1b53bf70b98a2:config"
)

# RHOAI Component Manifests
declare -A RHOAI_COMPONENT_MANIFESTS=(
    ["dashboard"]="red-hat-data-services:odh-dashboard:rhoai-3.5-ea.1@5423253bdf44aec7ee9f2548135234c9908d968b:manifests"
    ["workbenches/kf-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-3.5-ea.1@7cdda08d6851860a8f4cf51189039324e0f1748c:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-3.5-ea.1@7cdda08d6851860a8f4cf51189039324e0f1748c:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="red-hat-data-services:notebooks:rhoai-3.5-ea.1@102817888de3afd5a73e814aedd70e9dccd7104f:manifests"
    ["kserve"]="red-hat-data-services:kserve:rhoai-3.5-ea.1@2d0382f8cbd2658a5347eb637e712ee6d39fca0b:config"
    ["ray"]="red-hat-data-services:kuberay:rhoai-3.5-ea.1@83955a4b0cf52a08a44219489106393078e6fd49:ray-operator/config"
    ["trustyai"]="red-hat-data-services:trustyai-service-operator:rhoai-3.5-ea.1@0f8f29d7f85a5e1bc42428f9d7aa4fc1461e75ab:config"
    ["modelregistry"]="red-hat-data-services:model-registry-operator:rhoai-3.5-ea.1@054aae4d51f70d619eb231cf0291bcaa6f32a708:config"
    ["trainingoperator"]="red-hat-data-services:training-operator:rhoai-3.5-ea.1@6e3ac82eb990cc001c010b903db7d4f9879289b0:manifests"
    ["datasciencepipelines"]="red-hat-data-services:data-science-pipelines-operator:rhoai-3.5-ea.1@691226c8f4a3b7060fc15c03cddae611e88b6675:config"
    ["modelcontroller"]="red-hat-data-services:odh-model-controller:rhoai-3.5-ea.1@35624eff9365e4904113f11260780353d0f80240:config"
    ["feastoperator"]="red-hat-data-services:feast:rhoai-3.5-ea.1@13314edeb99dba402c6b6bc4e1ff54c458da2d27:infra/feast-operator/config"
    ["ogx"]="red-hat-data-services:ogx-k8s-operator:rhoai-3.5-ea.1@13dbef7f97a429ef2f11d7e148f091037b47e378:config"
    ["trainer"]="red-hat-data-services:trainer:rhoai-3.5-ea.1@138f5c9d590be0e7aa548798b20ae1b2fa5ac6b9:manifests"
    ["maas"]="red-hat-data-services:maas-billing:rhoai-3.5-ea.1@2c9aa2195322d43721f45f624d161d634ff559c4:deployment"
    ["mlflowoperator"]="red-hat-data-services:mlflow-operator:rhoai-3.5-ea.1@3a6fc5522749b99afa95cce689efc5b0f759bb8e:config"
    ["sparkoperator"]="red-hat-data-services:spark-operator:rhoai-3.5-ea.1@9aae1325d1f2ef925013eefb80f3ec2606436038:config"
    ["wva"]="red-hat-data-services:workload-variant-autoscaler:rhoai-3.5-ea.1@416efb40625e0af0f76e570fd1e8be864a890fc2:config"
)

# {ODH,RHOAI}_COMPONENT_CHARTS are lists of chart repositories info to fetch helm charts
# in the same format as manifests: "repo-org:repo-name:ref-name:source-folder"
# key is the target folder under charts/

# ODH Component Charts
declare -A ODH_COMPONENT_CHARTS=(
    ["cert-manager-operator"]="opendatahub-io:odh-gitops:main@60dd6c6caa469386ea83c87c5927e486687a067c:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="opendatahub-io:odh-gitops:main@60dd6c6caa469386ea83c87c5927e486687a067c:charts/dependencies/lws-operator"
    ["sail-operator"]="opendatahub-io:odh-gitops:main@60dd6c6caa469386ea83c87c5927e486687a067c:charts/dependencies/sail-operator"
    ["gateway-api"]="opendatahub-io:odh-gitops:main@60dd6c6caa469386ea83c87c5927e486687a067c:charts/dependencies/gateway-api"
)

# RHOAI Component Charts
declare -A RHOAI_COMPONENT_CHARTS=(
    ["cert-manager-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5-ea.1@b8c51650cbca746d5f00c36fecfaca660b4efb8f:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5-ea.1@b8c51650cbca746d5f00c36fecfaca660b4efb8f:charts/dependencies/lws-operator"
    ["sail-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5-ea.1@b8c51650cbca746d5f00c36fecfaca660b4efb8f:charts/dependencies/sail-operator"
    ["gateway-api"]="red-hat-data-services:odh-gitops:rhoai-3.5-ea.1@b8c51650cbca746d5f00c36fecfaca660b4efb8f:charts/dependencies/gateway-api"
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
