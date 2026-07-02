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
    ["dashboard"]="opendatahub-io:odh-dashboard:main@f601afd246bd5551a52ca49ef69d1f69b928eac1:manifests"
    ["workbenches/kf-notebook-controller"]="opendatahub-io:kubeflow:main@5d0d761264ce34fa7dfaf5e1fff8fec78cfab892:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="opendatahub-io:kubeflow:main@5d0d761264ce34fa7dfaf5e1fff8fec78cfab892:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="opendatahub-io:notebooks:main@07ef1a4c72b954b61938a1283cbd4d0c83dafc68:manifests"
    ["kserve"]="opendatahub-io:kserve:release-v0.17@9049db705a4cecfed6850438dcffc7e07423effa:config"
    ["ray"]="opendatahub-io:kuberay:dev@c9b4bd7563cdd0d01ea45594926e03d1e43952e6:ray-operator/config"
    ["trustyai"]="opendatahub-io:trustyai-service-operator:incubation@c2fc7266af27709b1e16a2a265853b6c81a9d0cb:config"
    ["modelregistry"]="opendatahub-io:model-registry-operator:main@326592a3669f43112d15862331bcf14866c2baa8:config"
    ["trainingoperator"]="opendatahub-io:training-operator:stable@fc6f0f150c5728fcca8601a654d0a09324a8c121:manifests"
    ["datasciencepipelines"]="opendatahub-io:data-science-pipelines-operator:main@8f1fffc6f5ada79aa40bb47594422b9711188e8b:config"
    ["modelcontroller"]="opendatahub-io:odh-model-controller:incubating@9600707247308745116b60ac47ab02a2b65edd49:config"
    ["feastoperator"]="opendatahub-io:feast:stable@2a4bb8241189343337e16a508b6a4baf92cb17db:infra/feast-operator/config"
    ["ogx"]="opendatahub-io:ogx-k8s-operator:odh@0ba12ea60949e1b551cebe63c3dc8be2dd4c0bd1:config"
    ["trainer"]="opendatahub-io:trainer:stable@fc9f4315b4ba88cfa03fabf29a730532411bab4c:manifests"
    ["maas"]="opendatahub-io:models-as-a-service:stable@8d9766d9e1cbeb97f42b4fe32bd281294b5c1599:deployment"
    ["mlflowoperator"]="opendatahub-io:mlflow-operator:main@78ab84841cefc6327b13384cab23b39397f41d6d:config"
    ["sparkoperator"]="opendatahub-io:spark-operator:main@c267ab785747fc6a5a31e4e597279d45a57b518b:config"
    ["wva"]="opendatahub-io:workload-variant-autoscaler:main@4cf1c0c215a25803ae9e864a85d40fafe5b406c3:config"
    ["aigateway"]="opendatahub-io:ai-gateway-operator:main@91028820d07a3057cb5c9b19081b1b260767d846:config")

# RHOAI Component Manifests
declare -A RHOAI_COMPONENT_MANIFESTS=(
    ["dashboard"]="red-hat-data-services:odh-dashboard:rhoai-3.5@19d5cc1aa1acc3da3ee542fd3333905a5884dc8c:manifests"
    ["workbenches/kf-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-3.5@03b46d4b7851845f58301eb8ef7f4ac3701fc5d7:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-3.5@03b46d4b7851845f58301eb8ef7f4ac3701fc5d7:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="red-hat-data-services:notebooks:rhoai-3.5@c2005f0566ee150ce1380371abb94506c4689281:manifests"
    ["kserve"]="red-hat-data-services:kserve:rhoai-3.5@45ced356378b3ce8eaa1545e74ccc47aad408c30:config"
    ["ray"]="red-hat-data-services:kuberay:rhoai-3.5@ad8367e60f9547f1f14915865608014a1e3cc6f4:ray-operator/config"
    ["trustyai"]="red-hat-data-services:trustyai-service-operator:rhoai-3.5@43432b4875d9d8939b56170c37da538dd0d8805b:config"
    ["modelregistry"]="red-hat-data-services:model-registry-operator:rhoai-3.5@b573db5c8353a70923973ed14c0737987332d6e0:config"
    ["trainingoperator"]="red-hat-data-services:training-operator:rhoai-3.5@3ff070439e3b5af804603a9b714aca0a9ecda855:manifests"
    ["datasciencepipelines"]="red-hat-data-services:data-science-pipelines-operator:rhoai-3.5@735b27cf021cb6adb534369587aba16045fdb927:config"
    ["modelcontroller"]="red-hat-data-services:odh-model-controller:rhoai-3.5@9032cfeb297cf0a735beec0f8de535ffe3b7cc45:config"
    ["feastoperator"]="red-hat-data-services:feast:rhoai-3.5@c1cd3477048b6f7d2ddc89e8b5297babbba1b91f:infra/feast-operator/config"
    ["ogx"]="red-hat-data-services:ogx-k8s-operator:rhoai-3.5@85d6d48cd1c3384489b0b1984790c07a101466a3:config"
    ["trainer"]="red-hat-data-services:trainer:rhoai-3.5@759d07a26148663b99b320c6ea377113ac211dde:manifests"
    ["maas"]="red-hat-data-services:maas-billing:rhoai-3.5@dd388bc4b3e44bd469e42d4a919ff270ba0c7740:deployment"
    ["mlflowoperator"]="red-hat-data-services:mlflow-operator:rhoai-3.5@ce6daa7592167939465d638b803ae7d210f54636:config"
    ["sparkoperator"]="red-hat-data-services:spark-operator:rhoai-3.5@3aeaf23fac628f2b218d9f8ace9fe7de76b3500a:config"
    ["wva"]="red-hat-data-services:workload-variant-autoscaler:rhoai-3.5@d690529e69170498f149d0ab89050966dfea8a11:config"
    ["aigateway"]="red-hat-data-services:ai-gateway-operator:rhoai-3.5:config"
)

# {ODH,RHOAI}_{CCM,COMPONENT}_CHARTS are lists of chart repositories info to fetch helm charts
# in the same format as manifests: "repo-org:repo-name:ref-name:source-folder"
# key is the target folder under charts/
# CCM_CHARTS: charts deployed by the CloudManager controller (dependencies)
# COMPONENT_CHARTS: charts deployed by individual component controllers

# ODH CloudManager Charts
declare -A ODH_CCM_CHARTS=(
    ["cert-manager-operator"]="opendatahub-io:odh-gitops:main@5fe2714d0ecaf5fafaf414952caf521ea9ebdaaf:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="opendatahub-io:odh-gitops:main@5fe2714d0ecaf5fafaf414952caf521ea9ebdaaf:charts/dependencies/lws-operator"
    ["sail-operator"]="opendatahub-io:odh-gitops:main@5fe2714d0ecaf5fafaf414952caf521ea9ebdaaf:charts/dependencies/sail-operator"
    ["gateway-api"]="opendatahub-io:odh-gitops:main@5fe2714d0ecaf5fafaf414952caf521ea9ebdaaf:charts/dependencies/gateway-api"
)

# ODH Component Charts
declare -A ODH_COMPONENT_CHARTS=(
)

# RHOAI CloudManager Charts
declare -A RHOAI_CCM_CHARTS=(
    ["cert-manager-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5@c47e419d99f07a527a3db6b6002f9b54819da1ee:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5@c47e419d99f07a527a3db6b6002f9b54819da1ee:charts/dependencies/lws-operator"
    ["sail-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5@c47e419d99f07a527a3db6b6002f9b54819da1ee:charts/dependencies/sail-operator"
    ["gateway-api"]="red-hat-data-services:odh-gitops:rhoai-3.5@c47e419d99f07a527a3db6b6002f9b54819da1ee:charts/dependencies/gateway-api"
)

# RHOAI Component Charts
declare -A RHOAI_COMPONENT_CHARTS=(
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
