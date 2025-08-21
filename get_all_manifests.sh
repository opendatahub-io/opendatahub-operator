#!/usr/bin/env bash
set -e

GITHUB_URL="https://github.com"

if [ "${ODH_PLATFORM_TYPE:-OpenDataHub}" = "OpenDataHub" ]; then
    GITHUB_ORG=opendatahub-io
    DEFAULT_REF=main
    MODELMESH_SERVING_REF=release-0.12.0-rc0
    KSERVE_REF=release-v0.15
    KUEUE_REF=dev
    RAY_REF=dev
    TRUSTYAI_REF=incubation
    TRAINING_REF=dev
    MODELCONTROLLER_REF=incubating
    FEAST_REF=stable
    LLAMASTACK_REF=odh

    echo "Cloning manifests for ODH"
else
    GITHUB_ORG=red-hat-data-services
    DEFAULT_REF="rhoai-$(echo $VERSION | sed 's/\([0-9]\+\).\([0-9]\+\).*/\1.\2/')"
    MODELMESH_SERVING_REF=$DEFAULT_REF
    KSERVE_REF=$DEFAULT_REF
    KUEUE_REF=$DEFAULT_REF
    RAY_REF=$DEFAULT_REF
    TRUSTYAI_REF=$DEFAULT_REF
    TRAINING_REF=$DEFAULT_REF
    MODELCONTROLLER_REF=$DEFAULT_REF
    FEAST_REF=$DEFAULT_REF
    LLAMASTACK_REF=$DEFAULT_REF

    echo "Cloning manifests for RHOAI using ref $DEFAULT_REF"
fi

# COMPONENT_MANIFESTS is a list of components repositories info to fetch the manifests
# in the format of "repo-org:repo-name:ref-name:source-folder" and key is the target folder under manifests/
declare -A COMPONENT_MANIFESTS=(
    ["dashboard"]="$GITHUB_ORG:odh-dashboard:$DEFAULT_REF:manifests"
    ["workbenches/kf-notebook-controller"]="$GITHUB_ORG:kubeflow:$DEFAULT_REF:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="$GITHUB_ORG:kubeflow:$DEFAULT_REF:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="$GITHUB_ORG:notebooks:$DEFAULT_REF:manifests"
    ["modelmeshserving"]="$GITHUB_ORG:modelmesh-serving:$MODELMESH_SERVING_REF:config"
    ["kserve"]="$GITHUB_ORG:kserve:$KSERVE_REF:config"
    ["kueue"]="$GITHUB_ORG:kueue:$KUEUE_REF:config"
    ["codeflare"]="$GITHUB_ORG:codeflare-operator:$DEFAULT_REF:config"
    ["ray"]="$GITHUB_ORG:kuberay:$RAY_REF:ray-operator/config"
    ["trustyai"]="$GITHUB_ORG:trustyai-service-operator:$TRUSTYAI_REF:config"
    ["modelregistry"]="$GITHUB_ORG:model-registry-operator:$DEFAULT_REF:config"
    ["trainingoperator"]="$GITHUB_ORG:training-operator:$TRAINING_REF:manifests"
    ["datasciencepipelines"]="$GITHUB_ORG:data-science-pipelines-operator:$DEFAULT_REF:config"
    ["modelcontroller"]="$GITHUB_ORG:odh-model-controller:$MODELCONTROLLER_REF:config"
    ["feastoperator"]="$GITHUB_ORG:feast:$FEAST_REF:infra/feast-operator/config"
    ["llamastackoperator"]="$GITHUB_ORG:llama-stack-k8s-operator:$LLAMASTACK_REF:config"
)

# PLATFORM_MANIFESTS is a list of manifests that are contained in the operator repository. Please also add them to the
# Dockerfile COPY instructions. Declaring them here causes this script to create a symlink in the manifests folder, so
# they can be easily modified during development, but during a container build, they must be copied into the proper
# location instead, as this script DOES NOT manage platform manifest files for a container build.
declare -A PLATFORM_MANIFESTS=(
    ["osd-configs"]="odh-config/osd-configs"
    ["monitoring"]="odh-config/monitoring"
    ["kueue-configs"]="odh-config/kueue-configs"
)

# Allow overwriting repo using flags component=repo
pattern="^[a-zA-Z0-9_.-]+:[a-zA-Z0-9_.-]+:[a-zA-Z0-9_.-]+:[a-zA-Z0-9_./-]+$"
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

function git_fetch_ref()
{

    local repo=$1
    local ref=$2
    local dir=$3
    local git_fetch="git fetch -q --depth 1 $repo"

    mkdir -p $dir
    pushd $dir &>/dev/null
    git init -q
    # try tag first, avoid printing fatal: couldn't find remote ref
    if ! $git_fetch refs/tags/$ref 2>/dev/null ; then
        $git_fetch refs/heads/$ref
    fi
    git reset -q --hard FETCH_HEAD
    popd &>/dev/null
}

download_manifest() {
    local key=$1
    local repo_info=$2
    echo -e "\033[32mCloning repo \033[33m${key}\033[32m:\033[0m ${repo_info}"
    IFS=':' read -r -a repo_info <<< "${repo_info}"

    repo_org="${repo_info[0]}"
    repo_name="${repo_info[1]}"
    repo_ref="${repo_info[2]}"
    source_path="${repo_info[3]}"
    target_path="${key}"

    repo_url="${GITHUB_URL}/${repo_org}/${repo_name}"
    repo_dir=${TMP_DIR}/${key}

    if [[ -v USE_LOCAL ]] && [[ -e ../${repo_name} ]]; then
        echo "copying from adjacent checkout ..."
        mkdir -p ./opt/manifests/${target_path}
        cp -rf "../${repo_name}/${source_path}"/* ./opt/manifests/${target_path}
        return
    fi

    git_fetch_ref ${repo_url} ${repo_ref} ${repo_dir}

    mkdir -p ./opt/manifests/${target_path}
    cp -rf ${repo_dir}/${source_path}/* ./opt/manifests/${target_path}
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

for key in "${!PLATFORM_MANIFESTS[@]}"; do
    source_path="${PLATFORM_MANIFESTS[$key]}"
    target_path="${key}"

    if [[ -d ${source_path} && ! -L ./opt/manifests/${target_path} ]]; then
        echo -e "\033[32mSymlinking local manifest \033[33m${key}\033[32m:\033[0m ${source_path}"
        ln -s $(pwd)/${source_path} ./opt/manifests/${target_path}
    fi
done
