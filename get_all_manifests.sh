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
    ["dashboard"]="andrewballantyne:odh-dashboard:drop-managed-addon-manifests:manifests"
    ["workbenches/kf-notebook-controller"]="opendatahub-io:kubeflow:main@ec116787be830a287a314e370ff732cdfffda873:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="opendatahub-io:kubeflow:main@ec116787be830a287a314e370ff732cdfffda873:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="opendatahub-io:notebooks:main@a7fb323e748a9c720de2bca7feb4566e7e22a99c:manifests"
    ["kserve"]="opendatahub-io:kserve:release-v0.17@6effd2fc4006d07602b3134fe37d33610eda7bfc:config"
    ["ray"]="opendatahub-io:kuberay:dev@05169e64f7a41f72076c9d3a41eb5ae0ff5e083b:ray-operator/config"
    ["trustyai"]="opendatahub-io:trustyai-service-operator:incubation@958611b13f5a62da76cc6933d83bf97fe3b36181:config"
    ["modelregistry"]="opendatahub-io:model-registry-operator:main@d56c75fadb1ee4aa2b162859055bf91734084a03:config"
    ["trainingoperator"]="opendatahub-io:training-operator:stable@28a60bd79b9dbbb39cd674d3660fa27ab1b42bdb:manifests"
    ["datasciencepipelines"]="opendatahub-io:data-science-pipelines-operator:main@2817bdf9613754dac1961dffa738007de3b398da:config"
    ["modelcontroller"]="opendatahub-io:odh-model-controller:incubating@6f0cf6e87512953761c199bfd65366ce9ee378ce:config"
    ["feastoperator"]="opendatahub-io:feast:stable@d7733ac6ab59274f07c8d583e48f1b08636f8997:infra/feast-operator/config"
    ["llamastackoperator"]="opendatahub-io:llama-stack-k8s-operator:odh@ba8020a4fc5b6ac86e14aea251992ee2ccdde5ef:config"
    ["trainer"]="opendatahub-io:trainer:stable@cc7a343111358bf1c1c8fccc73db141ebda521e2:manifests"
    ["maas"]="opendatahub-io:models-as-a-service:stable@44afbe21de59248e96448aa941af9811e339a68f:deployment"
    ["mlflowoperator"]="opendatahub-io:mlflow-operator:main@9a0ea2c59dc32a19ad60a648ea61d1b33659cc17:config"
    ["sparkoperator"]="opendatahub-io:spark-operator:main@49a9947eadc1d5cf5bab92e7af5169ba4d677726:config"
    ["wva"]="opendatahub-io:workload-variant-autoscaler:main@3c221bc56e7303daf8e9aa6ac671012131764a3c:config"
)

# RHOAI Component Manifests
declare -A RHOAI_COMPONENT_MANIFESTS=(
    ["dashboard"]="andrewballantyne:odh-dashboard:drop-managed-addon-manifests:manifests"
    ["workbenches/kf-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-3.5-ea.1@32b9d3643d7a5c9341882ceb96c62ed4abd6f255:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-3.5-ea.1@32b9d3643d7a5c9341882ceb96c62ed4abd6f255:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="red-hat-data-services:notebooks:rhoai-3.5-ea.1@e297f8b4816505e7a4f2ebbf8c7b9b4f59c4723c:manifests"
    ["kserve"]="red-hat-data-services:kserve:rhoai-3.5-ea.1@a0331d5707e0adda0c2dc0c94b3c9826e0cec45b:config"
    ["ray"]="red-hat-data-services:kuberay:rhoai-3.5-ea.1@5aa1935387c5d73b394fe6e3baa76c97aebe6992:ray-operator/config"
    ["trustyai"]="red-hat-data-services:trustyai-service-operator:rhoai-3.5-ea.1@addb3370eeaadb263a0c19d9d08241ece299bc92:config"
    ["modelregistry"]="red-hat-data-services:model-registry-operator:rhoai-3.5-ea.1@e16f70cb7fd8b922d2942b90eb7fbb68c2445f68:config"
    ["trainingoperator"]="red-hat-data-services:training-operator:rhoai-3.5-ea.1@32e964c3fb2b56e8b45474f72a5696e6d96a1fcf:manifests"
    ["datasciencepipelines"]="red-hat-data-services:data-science-pipelines-operator:rhoai-3.5-ea.1@9cf998a9ecf9cf36a57e8b40d855f4077bc9362d:config"
    ["modelcontroller"]="red-hat-data-services:odh-model-controller:rhoai-3.5-ea.1@1d9c9f238d231f1348aebed86fc5b460057b9f4c:config"
    ["feastoperator"]="red-hat-data-services:feast:rhoai-3.5-ea.1@0e31ddf9b98cd1fa1748cb209e2d7bfd02868ab5:infra/feast-operator/config"
    ["llamastackoperator"]="red-hat-data-services:llama-stack-k8s-operator:rhoai-3.5-ea.1@b4f40fe4ed716438d06d4a24d76d14b0db35d397:config"
    ["trainer"]="red-hat-data-services:trainer:rhoai-3.5-ea.1@7188fb78e8441ccb6cb2581990ca29a09a5f8aa9:manifests"
    ["maas"]="red-hat-data-services:maas-billing:rhoai-3.5-ea.1@d0ff49ba63903b5432dbb97955a8d1591a19d07b:deployment"
    ["mlflowoperator"]="red-hat-data-services:mlflow-operator:rhoai-3.5-ea.1@f2a8ac494c24149ee00d7e3d896a501f87e097a1:config"
    ["sparkoperator"]="red-hat-data-services:spark-operator:rhoai-3.5-ea.1@885bae36391b67e04accc3fa351c683dc40c8b77:config"
    ["wva"]="red-hat-data-services:workload-variant-autoscaler:rhoai-3.5-ea.1@3a8be828644366601062cd332f8d37609797eb4e:config"
)

# {ODH,RHOAI}_COMPONENT_CHARTS are lists of chart repositories info to fetch helm charts
# in the same format as manifests: "repo-org:repo-name:ref-name:source-folder"
# key is the target folder under charts/

# ODH Component Charts
declare -A ODH_COMPONENT_CHARTS=(
    ["cert-manager-operator"]="opendatahub-io:odh-gitops:main@dd82c46b320340f5f981ddf69668c814f7ffdc52:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="opendatahub-io:odh-gitops:main@dd82c46b320340f5f981ddf69668c814f7ffdc52:charts/dependencies/lws-operator"
    ["sail-operator"]="opendatahub-io:odh-gitops:main@dd82c46b320340f5f981ddf69668c814f7ffdc52:charts/dependencies/sail-operator"
    ["gateway-api"]="opendatahub-io:odh-gitops:main@dd82c46b320340f5f981ddf69668c814f7ffdc52:charts/dependencies/gateway-api"
)

# RHOAI Component Charts
declare -A RHOAI_COMPONENT_CHARTS=(
    ["cert-manager-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5-ea.1@95715fb76fb24d6775e6e4136a96a58ac18e40a1:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5-ea.1@95715fb76fb24d6775e6e4136a96a58ac18e40a1:charts/dependencies/lws-operator"
    ["sail-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5-ea.1@95715fb76fb24d6775e6e4136a96a58ac18e40a1:charts/dependencies/sail-operator"
    ["gateway-api"]="red-hat-data-services:odh-gitops:rhoai-3.5-ea.1@95715fb76fb24d6775e6e4136a96a58ac18e40a1:charts/dependencies/gateway-api"
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
