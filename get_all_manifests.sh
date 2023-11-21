#!/bin/bash
set -e

GITHUB_URL="https://github.com/"
# update to use different git repo for legacy manifests
MANIFEST_ORG="red-hat-data-services"
# comment out below logic once we have all component manifests ready to get from source git repo
MANIFEST_RELEASE="master"
MANIFESTS_TARBALL_URL="${GITHUB_URL}/${MANIFEST_ORG}/odh-manifests/tarball/${MANIFEST_RELEASE}"

# component: notebook, dsp, kserve, dashbaord, cf/ray, trustyai, modelmesh.
# in the format of "repo-org:repo-name:branch-name:source-folder:target-folder".
declare -A COMPONENT_MANIFESTS=(
    ["codeflare"]="red-hat-data-services:codeflare-operator:rhods-2.5:config:codeflare"
    ["ray"]="red-hat-data-services:kuberay:rhods-2.5:ray-operator/config:ray"
    ["data-science-pipelines-operator"]="red-hat-data-services:data-science-pipelines-operator:rhods-2.5:config:data-science-pipelines-operator"
    ["kf-notebook-controller"]="red-hat-data-services:kubeflow:rhods-2.5:components/notebook-controller/config:odh-notebook-controller/kf-notebook-controller"
    ["odh-notebook-controller"]="red-hat-data-services:kubeflow:rhods-2.5:components/odh-notebook-controller/config:odh-notebook-controller/odh-notebook-controller"
    ["notebooks"]="red-hat-data-services:notebooks:rhods-2.5:manifests:/jupyterhub/notebooks"
    ["trustyai"]="red-hat-data-services:trustyai-service-operator:rhods-2.5:config:trustyai-service-operator"
    ["model-mesh"]="red-hat-data-services:modelmesh-serving:rhods-2.5:config:model-mesh"
    ["odh-model-controller"]="red-hat-data-services:odh-model-controller:rhods-2.5:config:odh-model-controller"
    ["kserve"]="red-hat-data-services:kserve:rhods-2.5:config:kserve"
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

# pre-cleanup local env
rm -fr ./odh-manifests/* ./.odh-manifests-tmp/

mkdir -p ./.odh-manifests-tmp/ ./odh-manifests/
wget -q -c ${MANIFESTS_TARBALL_URL} -O - | tar -zxv -C ./.odh-manifests-tmp/ --strip-components 1 > /dev/null

# mm-monitroing
cp -r ./.odh-manifests-tmp/modelmesh-monitoring/ ./odh-manifests

# Dashboard
cp -r ./.odh-manifests-tmp/odh-dashboard/ ./odh-manifests/dashboard

rm -rf ${MANIFEST_RELEASE}.tar.gz ./.odh-manifests-tmp/

for key in "${!COMPONENT_MANIFESTS[@]}"; do
    echo "Cloning repo ${key}: ${COMPONENT_MANIFESTS[$key]}"
    IFS=':' read -r -a repo_info <<< "${COMPONENT_MANIFESTS[$key]}"

    repo_org="${repo_info[0]}"
    repo_name="${repo_info[1]}"
    repo_branch="${repo_info[2]}"
    source_path="${repo_info[3]}"
    target_path="${repo_info[4]}"

    repo_url="${GITHUB_URL}/${repo_org}/${repo_name}.git"
    rm -rf ./.${repo_name}
    git clone --depth 1 --branch ${repo_branch} ${repo_url} ./.${repo_name}
    mkdir -p ./odh-manifests/${target_path}
    cp -rf ./.${repo_name}/${source_path}/* ./odh-manifests/${target_path}
    rm -rf ./.${repo_name}
done