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
    ["dashboard"]="opendatahub-io:odh-dashboard:main@c758509fc35e2d100df39216f922e22416962dbb:manifests"
    ["workbenches/kf-notebook-controller"]="opendatahub-io:kubeflow:main@182ab1d1df30089f4b1633389821655753f8e59d:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="opendatahub-io:kubeflow:main@182ab1d1df30089f4b1633389821655753f8e59d:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="opendatahub-io:notebooks:main@1df9dc23708d24362043f69f2a4554d84ecab0aa:manifests"
    ["kserve"]="opendatahub-io:kserve:release-v0.17@e60cd1af33ab954bfd644ea141bff3544f6be886:config"
    ["ray"]="opendatahub-io:kuberay:dev@8c772ccfe6a9734d6152c8a24e270488b967231d:ray-operator/config"
    ["trustyai"]="opendatahub-io:trustyai-service-operator:incubation@c2fc7266af27709b1e16a2a265853b6c81a9d0cb:config"
    ["modelregistry"]="opendatahub-io:model-registry-operator:main@0e51b53d7173a34b8a8be64b2b87d90a21c53e75:config"
    ["trainingoperator"]="opendatahub-io:training-operator:stable@5bc66b3257a6280186d8689237257e7bba4fe749:manifests"
    ["datasciencepipelines"]="opendatahub-io:data-science-pipelines-operator:main@c408b720682f9e2009519aa4bd35ffe7275064aa:config"
    ["modelcontroller"]="opendatahub-io:odh-model-controller:incubating@3a9299290e26505c6655a5570f10019ceb81b8e7:config"
    ["feastoperator"]="opendatahub-io:feast:stable@bad000a710030ca06f42741715b7b215a443c85c:infra/feast-operator/config"
    ["ogx"]="opendatahub-io:ogx-k8s-operator:odh@fb523826ec4c6530a800a9b786c76069033233e6:config"
    ["trainer"]="opendatahub-io:trainer:stable@fc9f4315b4ba88cfa03fabf29a730532411bab4c:manifests"
    ["maas"]="opendatahub-io:models-as-a-service:stable@2e78401e96428d8b846d7b37b1cb95665e9c82c6:deployment"
    ["mlflowoperator"]="opendatahub-io:mlflow-operator:main@752154e46db1c953d70bf06f4d54417a21763543:config"
    ["sparkoperator"]="opendatahub-io:spark-operator:main@2cbd4dcee990d9ef018566dde5dad74ce5de3d45:config"
    ["wva"]="opendatahub-io:workload-variant-autoscaler:main@3400bc349e26e6ec25d27c86649ea64e80fcf0ea:config"
    ["aigateway"]="opendatahub-io:ai-gateway-operator:stable@6425cd676043f78c6d14ecd2679f85563c0fe975:config"
    ["mcplifecycleoperator"]="opendatahub-io:mcp-lifecycle-module-operator:main@8a35318566a4b677d8bd5e86cce5156be49f4e74:config"
)

# RHOAI Component Manifests
declare -A RHOAI_COMPONENT_MANIFESTS=(
    ["dashboard"]="red-hat-data-services:odh-dashboard:rhoai-3.5@04cf5bfc0e8d2fa3b0cdfe5967acbb25e5ff1e01:manifests"
    ["workbenches/kf-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-3.5@db355aa9ecca48c70748ede9261edfc6ad3d288b:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-3.5@db355aa9ecca48c70748ede9261edfc6ad3d288b:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="red-hat-data-services:notebooks:rhoai-3.5@e091c5ed0abe9bc7f45406b6f93121c229d7f87f:manifests"
    ["kserve"]="red-hat-data-services:kserve:rhoai-3.5@14d1b2d06d87f6afff8231d7f2fd205d45537d4c:config"
    ["ray"]="red-hat-data-services:kuberay:rhoai-3.5@eee280e58d9abf20e9bf0d23616d684850252813:ray-operator/config"
    ["trustyai"]="red-hat-data-services:trustyai-service-operator:rhoai-3.5@572af228f30b404f2d89c19694465ae545402a7d:config"
    ["modelregistry"]="red-hat-data-services:model-registry-operator:rhoai-3.5@61dab336cb2fed40e8daa2023785179a1bebd360:config"
    ["trainingoperator"]="red-hat-data-services:training-operator:rhoai-3.5@d85e23bb747b0c50cefffd62deed13b922fb4720:manifests"
    ["datasciencepipelines"]="red-hat-data-services:data-science-pipelines-operator:rhoai-3.5@a245ad25e928eef64ee783bca12b5287e86b6a4f:config"
    ["modelcontroller"]="red-hat-data-services:odh-model-controller:rhoai-3.5@0ab9a190bec063ffc7a2fc21d92a67734b46b179:config"
    ["feastoperator"]="red-hat-data-services:feast:rhoai-3.5@280859e613a89c19cac50483b5ee13384d23113f:infra/feast-operator/config"
    ["ogx"]="red-hat-data-services:ogx-k8s-operator:rhoai-3.5@776baa61b16e5986e775a7d51c3efdcce252aae4:config"
    ["trainer"]="red-hat-data-services:trainer:rhoai-3.5@12bb9b04a21827d633ac99ae65517c97d36457c7:manifests"
    ["maas"]="red-hat-data-services:maas-billing:rhoai-3.5@9dae594fbbd5faff1b6db7c05ea626519d78c03e:deployment"
    ["mlflowoperator"]="red-hat-data-services:mlflow-operator:rhoai-3.5@618c7cd4761fe8590e4ec946d931f5179396e7bf:config"
    ["sparkoperator"]="red-hat-data-services:spark-operator:rhoai-3.5@097e5b49586299da1e6af8b6a886a220652edaa0:config"
    ["wva"]="red-hat-data-services:workload-variant-autoscaler:rhoai-3.5@46caa7bf9f30b3577aceba34bdd30e996dc02b98:config"
    ["aigateway"]="red-hat-data-services:ai-gateway-operator:rhoai-3.5@28a185925793953f02c239c94058f69baf323ede:config"
    ["mcplifecycleoperator"]="red-hat-data-services:mcp-lifecycle-module-operator:rhoai-3.5@36fb8e59184668f5b5789aaf815ba5deda0f2279:config"
)

# {ODH,RHOAI}_{CCM,COMPONENT}_CHARTS are lists of chart repositories info to fetch helm charts
# in the same format as manifests: "repo-org:repo-name:ref-name:source-folder"
# key is the target folder under charts/
# CCM_CHARTS: charts deployed by the CloudManager controller (dependencies)
# COMPONENT_CHARTS: charts deployed by individual component controllers

# ODH CloudManager Charts
declare -A ODH_CCM_CHARTS=(
    ["cert-manager-operator"]="opendatahub-io:odh-gitops:main@73c78277878bb3920f2fe5c4a6f64f66c30eebfe:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="opendatahub-io:odh-gitops:main@73c78277878bb3920f2fe5c4a6f64f66c30eebfe:charts/dependencies/lws-operator"
    ["sail-operator"]="opendatahub-io:odh-gitops:main@73c78277878bb3920f2fe5c4a6f64f66c30eebfe:charts/dependencies/sail-operator"
    ["gateway-api"]="opendatahub-io:odh-gitops:main@73c78277878bb3920f2fe5c4a6f64f66c30eebfe:charts/dependencies/gateway-api"
)

# ODH Component Charts
declare -A ODH_COMPONENT_CHARTS=(
)

# RHOAI CloudManager Charts
declare -A RHOAI_CCM_CHARTS=(
    ["cert-manager-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5@32fa1480e8d5b1a41f7f66923e6bdcd0057d37a5:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5@32fa1480e8d5b1a41f7f66923e6bdcd0057d37a5:charts/dependencies/lws-operator"
    ["sail-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5@32fa1480e8d5b1a41f7f66923e6bdcd0057d37a5:charts/dependencies/sail-operator"
    ["gateway-api"]="red-hat-data-services:odh-gitops:rhoai-3.5@32fa1480e8d5b1a41f7f66923e6bdcd0057d37a5:charts/dependencies/gateway-api"
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
