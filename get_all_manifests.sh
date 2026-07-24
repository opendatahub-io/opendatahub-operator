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
    ["dashboard"]="opendatahub-io:odh-dashboard:main@77e111f67c349ca5998da288679f6163625009ca:manifests"
    ["kserve"]="opendatahub-io:kserve:release-v0.17@062fe761bf57a1755bcbd6da462a8b4e9e13f262:config"
    ["kserve-module-operator"]="opendatahub-io:kserve:release-v0.17@062fe761bf57a1755bcbd6da462a8b4e9e13f262:kserve-module/config"
    ["ray"]="opendatahub-io:kuberay:dev@6df5eac0ee71bd6245fa8cdbc577aa689bd3aa39:ray-operator/config"
    ["trustyai"]="opendatahub-io:trustyai-service-operator:incubation@f79b197393d44e6a5e70e478a913de0e9ab2d78e:config"
    ["modelregistry"]="opendatahub-io:model-registry-operator:main@e7a49066f46f12f0939d79c8807aa1513f6df6b6:config"
    ["trainingoperator"]="opendatahub-io:training-operator:stable@f6c57e481ca17a27bca4d4324240fcd245770287:manifests"
    ["datasciencepipelines"]="opendatahub-io:data-science-pipelines-operator:main@ed98cd55e9d094d5928dc3723e491bf04252b1ab:config"
    ["modelcontroller"]="opendatahub-io:odh-model-controller:incubating@57b43aad41aea718861b6c70b38a16794fc4ac8e:config"
    ["feastoperator"]="opendatahub-io:feast:stable@f41c15a94687684fc800f526d6fa43c02bf43d51:infra/feast-operator/config"
    ["ogx"]="opendatahub-io:ogx-k8s-operator:odh@efc37a8c95b4ce1319fd5ddc7ae42a3c0cbbe386:config"
    ["trainer"]="opendatahub-io:trainer:stable@d063877c480bbd9a35512691ded96c97d8937b44:manifests"
    ["maas"]="opendatahub-io:models-as-a-service:stable@7f86f55736c5192dda9d39dd8c69ee076f23a59e:deployment"
    ["mlflowoperator"]="opendatahub-io:mlflow-operator:main@c08c09e8d9298d1ed1a5d0d2a00898f5fd4ee2e2:config"
    ["sparkoperator"]="opendatahub-io:spark-operator:main@807b949ffc7e3e681462abdf8aa89d319f8cc957:config"
    ["wva"]="opendatahub-io:workload-variant-autoscaler:main@d058e0d6de79251c90ebc7a8013397034fd5b516:config"
    ["aigateway"]="opendatahub-io:ai-gateway-operator:stable@767b5856cd9f41a19ad606aba33fce16f3c44db6:config"
    ["mcplifecycleoperator"]="opendatahub-io:mcp-lifecycle-module-operator:main@2ead0c491ec23d979e16a1f0af3d07035ac5ce88:config"
)

# RHOAI Component Manifests
declare -A RHOAI_COMPONENT_MANIFESTS=(
    ["dashboard"]="red-hat-data-services:odh-dashboard:rhoai-3.5@75c0e2f0d5959f43c16f3568a54a0eec6fc38ea0:manifests"
    ["kserve"]="red-hat-data-services:kserve:rhoai-3.5@55801b4448e8886929a64f1fe856bbc08a973183:config"
    ["kserve-module-operator"]="red-hat-data-services:kserve:rhoai-3.5@55801b4448e8886929a64f1fe856bbc08a973183:kserve-module/config"
    ["ray"]="red-hat-data-services:kuberay:rhoai-3.5@b6e2cb5afb65f14562a3d2821dfaa7b14366c1e4:ray-operator/config"
    ["trustyai"]="red-hat-data-services:trustyai-service-operator:rhoai-3.5@b76913c12d8fc6864b52a3f000a781182f5cf5cc:config"
    ["modelregistry"]="red-hat-data-services:model-registry-operator:rhoai-3.5@4918619c6baf437f9f3052a5daf8bd4aee008f44:config"
    ["trainingoperator"]="red-hat-data-services:training-operator:rhoai-3.5@ce3a66c2bbd69e9c66445e09ba397be6ae684819:manifests"
    ["datasciencepipelines"]="red-hat-data-services:data-science-pipelines-operator:rhoai-3.5@324aa96d3bad5891701b660e6c47cf69fd8207c8:config"
    ["modelcontroller"]="red-hat-data-services:odh-model-controller:rhoai-3.5@660045635a94b1d54e35bd29254adc79a30799b0:config"
    ["feastoperator"]="red-hat-data-services:feast:rhoai-3.5@b6e78d0291d26c1dd44b0dd30b29d421efb21d27:infra/feast-operator/config"
    ["ogx"]="red-hat-data-services:ogx-k8s-operator:rhoai-3.5@4cf83e7e397f29cb1e7cf6c59528bcfffb4a94c6:config"
    ["trainer"]="red-hat-data-services:trainer:rhoai-3.5@7280dd4483ff21406ec62c8a5c442511755ce616:manifests"
    ["maas"]="red-hat-data-services:models-as-a-service:rhoai-3.5@d1b5d4015bbb4523e90d6187b0a0c0e6d0ae0977:deployment"
    ["mlflowoperator"]="red-hat-data-services:mlflow-operator:rhoai-3.5@58139bd2fd4206c0235c3fbdd5fa342ccda5bb91:config"
    ["sparkoperator"]="red-hat-data-services:spark-operator:rhoai-3.5@7b15494e3af5a29e599adde04e7d612759de1122:config"
    ["wva"]="red-hat-data-services:workload-variant-autoscaler:rhoai-3.5@c96b3a10385cf0726f02fc48b031dc0da3f742c0:config"
    ["aigateway"]="red-hat-data-services:ai-gateway-operator:rhoai-3.5@b99ff4c99afab9d06117a65387b8fe8c7d1188da:config"
    ["mcplifecycleoperator"]="red-hat-data-services:mcp-lifecycle-module-operator:rhoai-3.5@67e83d532d714f3c494c4303721f7c052dffee8c:config"
)

# {ODH,RHOAI}_{CCM,COMPONENT}_CHARTS are lists of chart repositories info to fetch helm charts
# in the same format as manifests: "repo-org:repo-name:ref-name:source-folder"
# key is the target folder under charts/
# CCM_CHARTS: charts deployed by the CloudManager controller (dependencies)
# COMPONENT_CHARTS: charts deployed by individual component controllers

# ODH CloudManager Charts
declare -A ODH_CCM_CHARTS=(
    ["cert-manager-operator"]="opendatahub-io:odh-gitops:main@6cd5007b1e7e00186bc6fe481addd21b90e76bd0:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="opendatahub-io:odh-gitops:main@6cd5007b1e7e00186bc6fe481addd21b90e76bd0:charts/dependencies/lws-operator"
    ["sail-operator"]="opendatahub-io:odh-gitops:main@6cd5007b1e7e00186bc6fe481addd21b90e76bd0:charts/dependencies/sail-operator"
    ["gateway-api"]="opendatahub-io:odh-gitops:main@6cd5007b1e7e00186bc6fe481addd21b90e76bd0:charts/dependencies/gateway-api"
)

# ODH Component Charts
declare -A ODH_COMPONENT_CHARTS=(
    ["workbenches"]="opendatahub-io:workbenches-operator:main@c857821857adab3155801a1301dad4151cdbc4e9:charts/operator"
)

# RHOAI CloudManager Charts
declare -A RHOAI_CCM_CHARTS=(
    ["cert-manager-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5@fedc4c0cb90ee19c4b351663753745b001f04622:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5@fedc4c0cb90ee19c4b351663753745b001f04622:charts/dependencies/lws-operator"
    ["sail-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5@fedc4c0cb90ee19c4b351663753745b001f04622:charts/dependencies/sail-operator"
    ["gateway-api"]="red-hat-data-services:odh-gitops:rhoai-3.5@fedc4c0cb90ee19c4b351663753745b001f04622:charts/dependencies/gateway-api"
)

# RHOAI Component Charts
declare -A RHOAI_COMPONENT_CHARTS=(
    ["workbenches"]="red-hat-data-services:workbenches-operator:main@61fc8746a65b143818fa3b9983baae2e2b084441:charts/operator"
)

# merge_charts merges CCM and component charts into COMPONENT_CHARTS, failing on duplicate keys.
merge_charts() {
    local -n _ccm=$1
    local -n _comp=$2
    for k in "${!_ccm[@]}"; do
        if [[ -n "${_comp[$k]+x}" ]]; then
            echo "ERROR: duplicate chart key '$k' in CCM and component charts" >&2
            exit 1
        fi
        COMPONENT_CHARTS["$k"]="${_ccm[$k]}"
    done
    for k in "${!_comp[@]}"; do
        COMPONENT_CHARTS["$k"]="${_comp[$k]}"
    done
}

# Select the appropriate manifest based on platform type
if [ "${ODH_PLATFORM_TYPE:-OpenDataHub}" = "OpenDataHub" ]; then
    echo "Cloning manifests and charts for ODH"
    declare -A COMPONENT_MANIFESTS=()
    for key in "${!ODH_COMPONENT_MANIFESTS[@]}"; do
        COMPONENT_MANIFESTS["$key"]="${ODH_COMPONENT_MANIFESTS[$key]}"
    done
    declare -A COMPONENT_CHARTS=()
    merge_charts ODH_CCM_CHARTS ODH_COMPONENT_CHARTS
else
    echo "Cloning manifests and charts for RHOAI"
    declare -A COMPONENT_MANIFESTS=()
    for key in "${!RHOAI_COMPONENT_MANIFESTS[@]}"; do
        COMPONENT_MANIFESTS["$key"]="${RHOAI_COMPONENT_MANIFESTS[$key]}"
    done
    declare -A COMPONENT_CHARTS=()
    merge_charts RHOAI_CCM_CHARTS RHOAI_COMPONENT_CHARTS
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
