#!/usr/bin/env bash
set -e

GITHUB_URL="https://github.com"

# COMPONENT_MANIFESTS is a list of components repositories info to fetch the manifests
# in the format of "repo-org:repo-name:ref-name:source-folder" and key is the target folder under manifests/
declare -A COMPONENT_MANIFESTS=(
    ["dashboard"]="opendatahub-io:odh-dashboard:v2.29.3-odh-release:manifests"
    ["workbenches/kf-notebook-controller"]="opendatahub-io:kubeflow:v1.9.0-6:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="opendatahub-io:kubeflow:v1.9.0-6:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="opendatahub-io:notebooks:v1.27.0:manifests"
    ["modelmeshserving"]="opendatahub-io:modelmesh-serving:odh-v2.23:config"
    ["kserve"]="opendatahub-io:kserve:odh-v2.23:config"
    ["kueue"]="opendatahub-io:kueue:v0.8.3:config"
    ["codeflare"]="opendatahub-io:codeflare-operator:v1.13.0:config"
    ["ray"]="opendatahub-io:kuberay:v1.1.0-odh-2:ray-operator/config"
    ["trustyai"]="trustyai-explainability:trustyai-service-operator:release/1.31.1:config"
    ["modelregistry"]="opendatahub-io:model-registry-operator:release/v0.2.12:config"
    ["trainingoperator"]="opendatahub-io:training-operator:v1.8.0-odh-3:manifests"
    ["datasciencepipelines"]="opendatahub-io:data-science-pipelines-operator:release/v2.9.0:config"
    ["modelcontroller"]="opendatahub-io:odh-model-controller:odh-v2.23:config"
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


for key in "${!COMPONENT_MANIFESTS[@]}"; do
    echo -e "\033[32mCloning repo \033[33m${key}\033[32m:\033[0m ${COMPONENT_MANIFESTS[$key]}"
    IFS=':' read -r -a repo_info <<< "${COMPONENT_MANIFESTS[$key]}"

    repo_org="${repo_info[0]}"
    repo_name="${repo_info[1]}"
    repo_ref="${repo_info[2]}"
    source_path="${repo_info[3]}"
    target_path="${key}"

    repo_url="${GITHUB_URL}/${repo_org}/${repo_name}"
    repo_dir=${TMP_DIR}/${key}

    git_fetch_ref ${repo_url} ${repo_ref} ${repo_dir}

    mkdir -p ./opt/manifests/${target_path}
    cp -rf ${repo_dir}/${source_path}/* ./opt/manifests/${target_path}

done
