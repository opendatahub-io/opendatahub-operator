#!/usr/bin/env bash
set -e

GITHUB_URL="https://github.com/"
# update to use different git repo for legacy manifests
MANIFEST_ORG="red-hat-data-services"

# component: notebook, dsp, kserve, dashbaord, cf/ray/kueue/trainingoperator, trustyai, modelmesh.
# in the format of "repo-org:repo-name:ref-name:source-folder:target-folder".
declare -A COMPONENT_MANIFESTS=(
    ["codeflare"]="red-hat-data-services:codeflare-operator:rhoai-2.14:config:codeflare"
    ["ray"]="red-hat-data-services:kuberay:rhoai-2.14:ray-operator/config:ray"
    ["kueue"]="red-hat-data-services:kueue:rhoai-2.14:config:kueue"
    ["data-science-pipelines-operator"]="red-hat-data-services:data-science-pipelines-operator:rhoai-2.14:config:data-science-pipelines-operator"
    ["kf-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-2.14:components/notebook-controller/config:odh-notebook-controller/kf-notebook-controller"
    ["odh-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-2.14:components/odh-notebook-controller/config:odh-notebook-controller/odh-notebook-controller"
    ["notebooks"]="red-hat-data-services:notebooks:rhoai-2.14:manifests:notebooks"
    ["trustyai"]="red-hat-data-services:trustyai-service-operator:rhoai-2.14:config:trustyai-service-operator"
    ["model-mesh"]="red-hat-data-services:modelmesh-serving:rhoai-2.14:config:model-mesh"
    ["odh-model-controller"]="red-hat-data-services:odh-model-controller:rhoai-2.14:config:odh-model-controller"
    ["kserve"]="red-hat-data-services:kserve:rhoai-2.14:config:kserve"
    ["odh-dashboard"]="red-hat-data-services:odh-dashboard:rhoai-2.14:manifests:dashboard"
    ["trainingoperator"]="red-hat-data-services:training-operator:rhoai-2.14:manifests:trainingoperator"
)

# Allow overwriting repo using flags component=repo
pattern="^[a-zA-Z0-9_.-]+:[a-zA-Z0-9_.-]+:[a-zA-Z0-9_.-]+:[a-zA-Z0-9_./-]+:[a-zA-Z0-9_./-]+$"
if [ "$#" -ge 1 ]; then
    for arg in "$@"; do
        if [[ $arg == --* ]]; then
            arg="${arg:2}"  # Remove the '--' prefix
            IFS="=" read -r key value <<< "$arg"
            if [[ -n "${COMPONENT_MANIFESTS[$key]}" ]]; then
                if [[ ! $value =~ $pattern ]]; then
                    echo "ERROR: The value '$value' does not match the expected format 'repo-org:repo-name:branch-name:source-folder:target-folder'."
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
    target_path="${repo_info[4]}"

    repo_url="${GITHUB_URL}/${repo_org}/${repo_name}"
    repo_dir=${TMP_DIR}/${key}

    git_fetch_ref ${repo_url} ${repo_ref} ${repo_dir}

    mkdir -p ./opt/manifests/${target_path}
    cp -rf ${repo_dir}/${source_path}/* ./opt/manifests/${target_path}

done
