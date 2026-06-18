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
    ["dashboard"]="opendatahub-io:odh-dashboard:main@9ecff2f01f2893083c7fa615ea4323695a1a44b8:manifests"
    ["workbenches/kf-notebook-controller"]="opendatahub-io:kubeflow:main@e242bdb880c3853b88da088f454e38e5e291a2eb:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="opendatahub-io:kubeflow:main@e242bdb880c3853b88da088f454e38e5e291a2eb:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="opendatahub-io:notebooks:main@a80654e90440d822f1505719528d135d6a05a4c2:manifests"
    ["kserve"]="opendatahub-io:kserve:release-v0.17@46db783dbc5e93cc53586638e55ea77940f1a39b:config"
    ["ray"]="opendatahub-io:kuberay:dev@ad21f8c87bbc1a9efe9ff399194abbdafd65aa08:ray-operator/config"
    ["trustyai"]="opendatahub-io:trustyai-service-operator:incubation@975eae20830028af9b83e05a1575d6146db193ef:config"
    ["modelregistry"]="opendatahub-io:model-registry-operator:main@6b56ffdf84327624c607e19dcc66ce5ceedb4fef:config"
    ["trainingoperator"]="opendatahub-io:training-operator:stable@fc6f0f150c5728fcca8601a654d0a09324a8c121:manifests"
    ["datasciencepipelines"]="opendatahub-io:data-science-pipelines-operator:main@1a9395e8ac2bcfd70cfdacc609603a0ce69f291a:config"
    ["modelcontroller"]="opendatahub-io:odh-model-controller:incubating@88c94815232345ac6da17e0a21572846e39a7d9b:config"
    ["feastoperator"]="opendatahub-io:feast:stable@2a4bb8241189343337e16a508b6a4baf92cb17db:infra/feast-operator/config"
    ["ogx"]="opendatahub-io:ogx-k8s-operator:odh@adc424e70d192054acfab4c768680d37f5aaad5d:config"
    ["trainer"]="opendatahub-io:trainer:stable@9779bc3df2e62eb686198995843959d4b0e8bb96:manifests"
    ["maas"]="opendatahub-io:models-as-a-service:stable@e01c094491b8806bd6cdd75ddab7cc0e24188d3b:deployment"
    ["mlflowoperator"]="opendatahub-io:mlflow-operator:main@b55bcb6aa528ccecf8904745bb0218350b1ff453:config"
    ["sparkoperator"]="opendatahub-io:spark-operator:main@1f4c2a848c928c24d914139222d24858aae6cf32:config"
    ["wva"]="opendatahub-io:workload-variant-autoscaler:main@c5b2a5f2d6ecbd0a37b0798db0858c64b03e7675:config"
)

# RHOAI Component Manifests
declare -A RHOAI_COMPONENT_MANIFESTS=(
    ["dashboard"]="red-hat-data-services:odh-dashboard:rhoai-3.5-ea.2@9afb23d15ed31337b66a1f9096b6efced8f2d621:manifests"
    ["workbenches/kf-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-3.5-ea.2@1b626b14f2ece8c617ff75886bf4b9c348a8516c:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-3.5-ea.2@1b626b14f2ece8c617ff75886bf4b9c348a8516c:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="red-hat-data-services:notebooks:rhoai-3.5-ea.2@dc331b8ee80be352983b0c74f6379edc7e0ccb91:manifests"
    ["kserve"]="red-hat-data-services:kserve:rhoai-3.5-ea.2@9c7ad4c9fe8223266154931fd81545a7b8d25c3c:config"
    ["ray"]="red-hat-data-services:kuberay:rhoai-3.5-ea.2@ab0d2b69fe910cde4692c220df6d02864ee36df9:ray-operator/config"
    ["trustyai"]="red-hat-data-services:trustyai-service-operator:rhoai-3.5-ea.2@114cd12190fe396aea9cd719d0fefa90f5ace125:config"
    ["modelregistry"]="red-hat-data-services:model-registry-operator:rhoai-3.5-ea.2@7c75bcad69f2b767924f0fd9debf2ccf398e2a59:config"
    ["trainingoperator"]="red-hat-data-services:training-operator:rhoai-3.5-ea.2@1eb40de7caa052dfd95c8319cb5c969fff16c246:manifests"
    ["datasciencepipelines"]="red-hat-data-services:data-science-pipelines-operator:rhoai-3.5-ea.2@e8775f3028a6a8573a07f8dbd63f37bb59445996:config"
    ["modelcontroller"]="red-hat-data-services:odh-model-controller:rhoai-3.5-ea.2@d6c5b5d80550fb8a34749bf84bf8f361c4f4f454:config"
    ["feastoperator"]="red-hat-data-services:feast:rhoai-3.5-ea.2@330bc12dd9874d30de57b6d0796b65d69c4be1d8:infra/feast-operator/config"
    ["ogx"]="red-hat-data-services:ogx-k8s-operator:rhoai-3.5-ea.2@e43e17b52faa32b759fb360b2eda4538f57c38fd:config"
    ["trainer"]="red-hat-data-services:trainer:rhoai-3.5-ea.2@1c8507b9562e526709da3594397486de757731b0:manifests"
    ["maas"]="red-hat-data-services:maas-billing:rhoai-3.5-ea.2@1312edee9de6730e315e710aad32d79eebdfe38d:deployment"
    ["mlflowoperator"]="red-hat-data-services:mlflow-operator:rhoai-3.5-ea.2@e17cb2f44a6cb0542b64cfac98466c9280d91b56:config"
    ["sparkoperator"]="red-hat-data-services:spark-operator:rhoai-3.5-ea.2@ac62e2ab746ba45ea16455da3e203deba986d9a4:config"
    ["wva"]="red-hat-data-services:workload-variant-autoscaler:rhoai-3.5-ea.2@93c9e78bd1671f577e5efe6ec9248b32d25f8124:config"
)

# {ODH,RHOAI}_COMPONENT_CHARTS are lists of chart repositories info to fetch helm charts
# in the same format as manifests: "repo-org:repo-name:ref-name:source-folder"
# key is the target folder under charts/

# ODH Component Charts
declare -A ODH_COMPONENT_CHARTS=(
    ["cert-manager-operator"]="opendatahub-io:odh-gitops:main@98a52397445a5a42f6799be48c7fb960e11383db:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="opendatahub-io:odh-gitops:main@98a52397445a5a42f6799be48c7fb960e11383db:charts/dependencies/lws-operator"
    ["sail-operator"]="opendatahub-io:odh-gitops:main@98a52397445a5a42f6799be48c7fb960e11383db:charts/dependencies/sail-operator"
    ["gateway-api"]="opendatahub-io:odh-gitops:main@98a52397445a5a42f6799be48c7fb960e11383db:charts/dependencies/gateway-api"
)

# RHOAI Component Charts
declare -A RHOAI_COMPONENT_CHARTS=(
    ["cert-manager-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5-ea.2@030f05060d9a5f7f9b3d5873af3f3fcaa15dc96b:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5-ea.2@030f05060d9a5f7f9b3d5873af3f3fcaa15dc96b:charts/dependencies/lws-operator"
    ["sail-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5-ea.2@030f05060d9a5f7f9b3d5873af3f3fcaa15dc96b:charts/dependencies/sail-operator"
    ["gateway-api"]="red-hat-data-services:odh-gitops:rhoai-3.5-ea.2@030f05060d9a5f7f9b3d5873af3f3fcaa15dc96b:charts/dependencies/gateway-api"
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
