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
    ["dashboard"]="opendatahub-io:odh-dashboard:main@b863ca8e87e31e74dea3f01b71c93401cff07045:manifests"
    ["workbenches/kf-notebook-controller"]="opendatahub-io:kubeflow:main@c6b9cc9fa59dda88dd37d6032a1e909a0d1140f4:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="opendatahub-io:kubeflow:main@c6b9cc9fa59dda88dd37d6032a1e909a0d1140f4:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="opendatahub-io:notebooks:main@2be5c1443475318566c62975b5015799176f7167:manifests"
    ["kserve"]="opendatahub-io:kserve:release-v0.17@d843557117dc584f82b82fb34489fa4d369924a0:config"
    ["ray"]="opendatahub-io:kuberay:dev@b5ee4c9963783dad6a8917abfa29c4e91d8630ec:ray-operator/config"
    ["trustyai"]="opendatahub-io:trustyai-service-operator:incubation@25e144dd0f4c311c53cbe069fed18fb93fdb3a7d:config"
    ["modelregistry"]="opendatahub-io:model-registry-operator:main@a6e2edef1f815e407d5447fe7d5c9c2d96f46c53:config"
    ["trainingoperator"]="opendatahub-io:training-operator:stable@28a60bd79b9dbbb39cd674d3660fa27ab1b42bdb:manifests"
    ["datasciencepipelines"]="opendatahub-io:data-science-pipelines-operator:main@e5d4c5a3251c9523c18f821015da2a72edb8e160:config"
    ["modelcontroller"]="opendatahub-io:odh-model-controller:incubating@cce594510895b0ef043a02e36011c39894553d39:config"
    ["feastoperator"]="opendatahub-io:feast:stable@59b34f5c8ec5bb6ea658687f96b795eebb16cbeb:infra/feast-operator/config"
    ["llamastackoperator"]="opendatahub-io:llama-stack-k8s-operator:odh@a5d32a8a739f0952e00fce008fbccbfff866ed76:config"
    ["trainer"]="opendatahub-io:trainer:stable@7fa0b8a212e8a7f6a99dbe1742cdca2309c61ea2:manifests"
    ["maas"]="opendatahub-io:maas-billing:main@572b783155c174d3fa0a31c7ceddbe7a0d9caf88:deployment"
    ["mlflowoperator"]="opendatahub-io:mlflow-operator:main@1e35d3471d2996ee1f42ac3088d7eb1f7a98b47a:config"
    ["sparkoperator"]="opendatahub-io:spark-operator:main@b9b6a255af50078c6dd7801979ffbb74c251a814:config"
    ["wva"]="opendatahub-io:workload-variant-autoscaler:main@a19027138bce452319a38a2aa46b19cca5ae2b97:config"
)

# RHOAI Component Manifests
declare -A RHOAI_COMPONENT_MANIFESTS=(
    ["dashboard"]="red-hat-data-services:odh-dashboard:rhoai-3.4@9fcbac5784f2c783076bc493ac2e9431fbb47568:manifests"
    ["workbenches/kf-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-3.4@9833234c8c82f941a429b71b6cfe22090b37ed00:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-3.4@9833234c8c82f941a429b71b6cfe22090b37ed00:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="red-hat-data-services:notebooks:rhoai-3.4@db1cb9048bd252ab1585280f9e3b03616146cf77:manifests"
    ["kserve"]="red-hat-data-services:kserve:rhoai-3.4@828fca57a0ece4b391878379b4a84f8a7cba0a8b:config"
    ["ray"]="red-hat-data-services:kuberay:rhoai-3.4@66f4a618d49b3b0d05b8dcb9af01bfa713b47ac8:ray-operator/config"
    ["trustyai"]="red-hat-data-services:trustyai-service-operator:rhoai-3.4@7f4bcdfda512f91d97c06fc20ba9d4923d6ed27e:config"
    ["modelregistry"]="red-hat-data-services:model-registry-operator:rhoai-3.4@2ea4f190970514ad75a6a63ea88b0aadf0e54407:config"
    ["trainingoperator"]="red-hat-data-services:training-operator:rhoai-3.4@b2441dcaa203a34c2ecd8c6a52ef52200cf2bd22:manifests"
    ["datasciencepipelines"]="red-hat-data-services:data-science-pipelines-operator:rhoai-3.4@c55dc0379561354f640ca4768e11bcfc2edfb389:config"
    ["modelcontroller"]="red-hat-data-services:odh-model-controller:rhoai-3.4@e44905bf9b4c61437faa70ee47c2dc225b015a21:config"
    ["feastoperator"]="red-hat-data-services:feast:rhoai-3.4@8283b4feb4065cf0e7fbb005c9f1be26f62199bc:infra/feast-operator/config"
    ["llamastackoperator"]="red-hat-data-services:llama-stack-k8s-operator:rhoai-3.4@44a5c4764c3f9143f0670c27541a16c64c613b76:config"
    ["trainer"]="red-hat-data-services:trainer:rhoai-3.4@fda21d2d81a3d900ded0236ccded6a02bd375597:manifests"
    ["maas"]="red-hat-data-services:maas-billing:rhoai-3.4@a434ef60d28c0d63585ea4ea02dfb1e363607ed5:deployment"
    ["mlflowoperator"]="red-hat-data-services:mlflow-operator:rhoai-3.4@4071895eb2c25e05a2d35d7a5c854be39154f7cb:config"
    ["sparkoperator"]="red-hat-data-services:spark-operator:rhoai-3.4@d628ba09eec76db802cdb174a4816e9f534707aa:config"
    ["wva"]="red-hat-data-services:workload-variant-autoscaler:rhoai-3.4-ea.2@a9492a1d6f75456d2032ecefcac6d65de88f7b3b:config"
)

# {ODH,RHOAI}_COMPONENT_CHARTS are lists of chart repositories info to fetch helm charts
# in the same format as manifests: "repo-org:repo-name:ref-name:source-folder"
# key is the target folder under charts/

# ODH Component Charts
declare -A ODH_COMPONENT_CHARTS=(
    ["cert-manager-operator"]="opendatahub-io:odh-gitops:main@0d31b409deac9d8ba720d459261ef3b341fbb232:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="opendatahub-io:odh-gitops:main@0d31b409deac9d8ba720d459261ef3b341fbb232:charts/dependencies/lws-operator"
    ["sail-operator"]="opendatahub-io:odh-gitops:main@0d31b409deac9d8ba720d459261ef3b341fbb232:charts/dependencies/sail-operator"
    ["gateway-api"]="opendatahub-io:odh-gitops:main@0d31b409deac9d8ba720d459261ef3b341fbb232:charts/dependencies/gateway-api"
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
