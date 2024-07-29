#!/bin/bash
set -e

GITHUB_URL="https://github.com"

# component: notebook, dsp, kserve, dashbaord, cf/ray/kueue/trainingoperator, trustyai, modelmesh, modelregistry.
# in the format of "repo-org:repo-name:branch-name:source-folder:target-folder".
declare -A COMPONENT_MANIFESTS=(
    ["codeflare"]="opendatahub-io:codeflare-operator:main:config:codeflare"
    ["ray"]="opendatahub-io:kuberay:dev:ray-operator/config:ray"
    ["kueue"]="opendatahub-io:kueue:dev:config:kueue"
    ["data-science-pipelines-operator"]="opendatahub-io:data-science-pipelines-operator:main:config:data-science-pipelines-operator"
    ["odh-dashboard"]="opendatahub-io:odh-dashboard:main:manifests:dashboard"
    ["kf-notebook-controller"]="opendatahub-io:kubeflow:v1.7-branch:components/notebook-controller/config:odh-notebook-controller/kf-notebook-controller"
    ["odh-notebook-controller"]="opendatahub-io:kubeflow:v1.7-branch:components/odh-notebook-controller/config:odh-notebook-controller/odh-notebook-controller"
    ["notebooks"]="opendatahub-io:notebooks:main:manifests:notebooks"
    ["trustyai"]="trustyai-explainability:trustyai-service-operator:main:config:trustyai-service-operator"
    ["model-mesh"]="opendatahub-io:modelmesh-serving:release-0.12.0-rc0:config:model-mesh"
    ["odh-model-controller"]="opendatahub-io:odh-model-controller:release-0.12.0:config:odh-model-controller"
    ["kserve"]="opendatahub-io:kserve:release-v0.12.1:config:kserve"
    ["modelregistry"]="opendatahub-io:model-registry-operator:main:config:model-registry-operator"
    ["trainingoperator"]="opendatahub-io:training-operator:dev:manifests:trainingoperator"
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


for key in "${!COMPONENT_MANIFESTS[@]}"; do
    echo -e "\033[32mCloning repo \033[33m${key}\033[32m:\033[0m ${COMPONENT_MANIFESTS[$key]}"
    IFS=':' read -r -a repo_info <<< "${COMPONENT_MANIFESTS[$key]}"

    repo_org="${repo_info[0]}"
    repo_name="${repo_info[1]}"
    repo_branch="${repo_info[2]}"
    source_path="${repo_info[3]}"
    target_path="${repo_info[4]}"

    repo_url="${GITHUB_URL}/${repo_org}/${repo_name}"
    repo_dir=${TMP_DIR}/${key}
    mkdir -p ${repo_dir}
    git clone -q --depth 1 --branch ${repo_branch} ${repo_url} ${repo_dir}

    mkdir -p ./opt/manifests/${target_path}
    cp -rf ${repo_dir}/${source_path}/* ./opt/manifests/${target_path}

done
