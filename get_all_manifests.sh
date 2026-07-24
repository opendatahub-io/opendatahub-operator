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
    ["dashboard"]="opendatahub-io:odh-dashboard:main@c5b6a7673390855925456adfcd49e4dac90607cb:manifests"
    ["kserve"]="opendatahub-io:kserve:release-v0.17@062fe761bf57a1755bcbd6da462a8b4e9e13f262:config"
    ["kserve-module-operator"]="opendatahub-io:kserve:release-v0.17@062fe761bf57a1755bcbd6da462a8b4e9e13f262:kserve-module/config"
    ["ray"]="opendatahub-io:kuberay:dev@c992b373f0b974e8df6e66743ddc59c08e0b93d5:ray-operator/config"
    ["trustyai"]="opendatahub-io:trustyai-service-operator:incubation@bc5156e764b2d360ca0eabff9e5131bb2c6ee672:config"
    ["modelregistry"]="opendatahub-io:model-registry-operator:main@ddaca00f1749175cef655b967c3c453fc0ef347f:config"
    ["trainingoperator"]="opendatahub-io:training-operator:stable@6f956df7cd33da24904b4469786a3cc2d6b7412c:manifests"
    ["datasciencepipelines"]="opendatahub-io:data-science-pipelines-operator:main@c408b720682f9e2009519aa4bd35ffe7275064aa:config"
    ["modelcontroller"]="opendatahub-io:odh-model-controller:incubating@e3cc4e63e2f08475ff878381637da9e44e356ad7:config"
    ["feastoperator"]="opendatahub-io:feast:stable@bad000a710030ca06f42741715b7b215a443c85c:infra/feast-operator/config"
    ["ogx"]="opendatahub-io:ogx-k8s-operator:odh@f3fd4620a572189092d26e7308a670a2cd0fc45a:config"
    ["trainer"]="opendatahub-io:trainer:stable@fc9f4315b4ba88cfa03fabf29a730532411bab4c:manifests"
    ["maas"]="opendatahub-io:models-as-a-service:stable@8c7449c59d08c9b5a7cd2120236b5eb871cfe179:deployment"
    ["mlflowoperator"]="opendatahub-io:mlflow-operator:main@d08ed9fdd2d0783900a9c679053ce87cf5649eb6:config"
    ["sparkoperator"]="opendatahub-io:spark-operator:main@4dd2dae8947d76dabfcbca3a38b525b8765741bd:config"
    ["wva"]="opendatahub-io:workload-variant-autoscaler:main@8df86ea0826a9fbb5339098f38a493dabf859c37:config"
    ["aigateway"]="opendatahub-io:ai-gateway-operator:stable@37ffeb78be2669bfc72bea0d9a3f6913cfe4dbad:config"
    ["mcplifecycleoperator"]="opendatahub-io:mcp-lifecycle-module-operator:main@88b399dc19350a0477126105aade516ef3f487f4:config"
)

# RHOAI Component Manifests
declare -A RHOAI_COMPONENT_MANIFESTS=(
    ["dashboard"]="red-hat-data-services:odh-dashboard:rhoai-3.5@5ed6ccb7d916b7ea9f1a6cf84173d4a20d8636d3:manifests"
    ["kserve"]="red-hat-data-services:kserve:rhoai-3.5@03e934e940248c6f592f823c5723a1150c726993:config"
    ["kserve-module-operator"]="red-hat-data-services:kserve:rhoai-3.5@0930ed0153b5853d1bdbfadf59ea92212652f79b:kserve-module/config"
    ["ray"]="red-hat-data-services:kuberay:rhoai-3.5@cf78580a6621af6bed8d99550781b94d1f5342bd:ray-operator/config"
    ["trustyai"]="red-hat-data-services:trustyai-service-operator:rhoai-3.5@26a8052cda820504e5e5383c0d8ef1d5ebd4f8b0:config"
    ["modelregistry"]="red-hat-data-services:model-registry-operator:rhoai-3.5@283975d584f886177b9371b9ba8a93163a76efe8:config"
    ["trainingoperator"]="red-hat-data-services:training-operator:rhoai-3.5@b8ae4ecfabe5ee1bff5991d20dfd66252a3d3b98:manifests"
    ["datasciencepipelines"]="red-hat-data-services:data-science-pipelines-operator:rhoai-3.5@324aa96d3bad5891701b660e6c47cf69fd8207c8:config"
    ["modelcontroller"]="red-hat-data-services:odh-model-controller:rhoai-3.5@6650d28e574f1a144939102e1a33af3756927875:config"
    ["feastoperator"]="red-hat-data-services:feast:rhoai-3.5@280859e613a89c19cac50483b5ee13384d23113f:infra/feast-operator/config"
    ["ogx"]="red-hat-data-services:ogx-k8s-operator:rhoai-3.5@e8fb654b06bb1532407336e701e2b11b175a9bf8:config"
    ["trainer"]="red-hat-data-services:trainer:rhoai-3.5@119ebe8902ee20051f00b08ab000d1ccb9aa39a1:manifests"
    ["maas"]="red-hat-data-services:models-as-a-service:rhoai-3.5@1e0df2e7b658424f20fbe18ceb8eba43c750d39a:deployment"
    ["mlflowoperator"]="red-hat-data-services:mlflow-operator:rhoai-3.5@d8d8e2c08749d96164ce400f6afc0ff768bc8c28:config"
    ["sparkoperator"]="red-hat-data-services:spark-operator:rhoai-3.5@77f949e1d3d438de39bc5f5ebccb45fdf0114852:config"
    ["wva"]="red-hat-data-services:workload-variant-autoscaler:rhoai-3.5@77a2f3ec16160dc9ec261511033260ff9402f0cf:config"
    ["aigateway"]="red-hat-data-services:ai-gateway-operator:rhoai-3.5@6fad31b02fcb8f3de26c867d78d841e2d8b9411a:config"
    ["mcplifecycleoperator"]="red-hat-data-services:mcp-lifecycle-module-operator:rhoai-3.5@58b4dc4cd63aa90eedb6386969962f5dbf7816d6:config"
)

# {ODH,RHOAI}_{CCM,COMPONENT}_CHARTS are lists of chart repositories info to fetch helm charts
# in the same format as manifests: "repo-org:repo-name:ref-name:source-folder"
# key is the target folder under charts/
# CCM_CHARTS: charts deployed by the CloudManager controller (dependencies)
# COMPONENT_CHARTS: charts deployed by individual component controllers

# ODH CloudManager Charts
declare -A ODH_CCM_CHARTS=(
    ["cert-manager-operator"]="opendatahub-io:odh-gitops:main@16827cc08236068d7ed1c6ff450eb28accda3ce1:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="opendatahub-io:odh-gitops:main@16827cc08236068d7ed1c6ff450eb28accda3ce1:charts/dependencies/lws-operator"
    ["sail-operator"]="opendatahub-io:odh-gitops:main@16827cc08236068d7ed1c6ff450eb28accda3ce1:charts/dependencies/sail-operator"
    ["gateway-api"]="opendatahub-io:odh-gitops:main@16827cc08236068d7ed1c6ff450eb28accda3ce1:charts/dependencies/gateway-api"
)

# ODH Component Charts
declare -A ODH_COMPONENT_CHARTS=(
    ["workbenches"]="opendatahub-io:workbenches-operator:main@5ee7fd8fb5aa155b3f5756e72b3f8e9d50693ba8:charts/operator"
)

# RHOAI CloudManager Charts
declare -A RHOAI_CCM_CHARTS=(
    ["cert-manager-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5@93fdfa3008901903026849c9d33cba78ee86db82:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5@93fdfa3008901903026849c9d33cba78ee86db82:charts/dependencies/lws-operator"
    ["sail-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5@93fdfa3008901903026849c9d33cba78ee86db82:charts/dependencies/sail-operator"
    ["gateway-api"]="red-hat-data-services:odh-gitops:rhoai-3.5@93fdfa3008901903026849c9d33cba78ee86db82:charts/dependencies/gateway-api"
)

# RHOAI Component Charts
declare -A RHOAI_COMPONENT_CHARTS=(
    ["workbenches"]="red-hat-data-services:workbenches-operator:main@ca84e8752199b63ae3ea2affa8aa747214546f23:charts/operator"
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
    # Disable LFS filter to avoid failures when git-lfs is not installed
    git config --local filter.lfs.smudge cat
    git config --local filter.lfs.process cat
    git config --local filter.lfs.required false

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
