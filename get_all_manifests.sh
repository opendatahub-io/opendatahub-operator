#!/bin/bash
set -e

# component: dsp, kserve, dashbaord, cf/ray. in the format of "repo-name:branch-name:source-folder:target-folder"
# TODO: workbench, modelmesh, monitoring, etc
REPO_LIST=(
    "distributed-workloads:main:codeflare-stack:codeflare"
    "distributed-workloads:main:ray:ray"
    "data-science-pipelines-operator:main:config:data-science-pipelines-operator"
   # "odh-dashboard:main:manifests:odh-dashboard"
   # "notebooks:main:manifests:notebook"
   # "kubeflow:master:components/notebook-controller/config:odh-notebook-controller/kf-notebook-controller"
   # "kubeflow:master:components/odh-notebook-controller/config:odh-notebook-controller/odh-notebook-controller"
)

# pre-cleanup local env
rm -fr ./odh-manifests/* ./.odh-manifests-tmp/

GITHUB_URL="https://github.com/"
# update to use different git repo
MANIFEST_ORG="red-hat-data-services"

# comment out below logic once we have all component manifests ready to get from source git repo
MANIFEST_RELEASE="master"
MANIFESTS_TARBALL_URL="${GITHUB_URL}/${MANIFEST_ORG}/odh-manifests/tarball/${MANIFEST_RELEASE}"
mkdir -p ./.odh-manifests-tmp/ ./odh-manifests/
wget -q -c ${MANIFESTS_TARBALL_URL} -O - | tar -zxv -C ./.odh-manifests-tmp/ --strip-components 1 > /dev/null
# modelmesh
cp -r ./.odh-manifests-tmp/model-mesh/ ./odh-manifests
cp -r ./.odh-manifests-tmp/odh-model-controller/ ./odh-manifests
cp -r ./.odh-manifests-tmp/modelmesh-monitoring/ ./odh-manifests
# Kserve
cp -r ./.odh-manifests-tmp/kserve/ ./odh-manifests
# workbench image
mkdir -p ./odh-manifests/jupyterhub/notebook-images
cp -r ./.odh-manifests-tmp/jupyterhub/notebook-images/* ./odh-manifests/jupyterhub/notebook-images
# workbench nbc
cp -r ./.odh-manifests-tmp/odh-notebook-controller/ ./odh-manifests
# Trustyai
# cp -r ./.odh-manifests-tmp/trustyai-service-operator ./odh-manifests
# Dashboard
cp -r ./.odh-manifests-tmp/odh-dashboard/ ./odh-manifests

for repo_info in ${REPO_LIST[@]}; do
    echo "Git clone below repo ${repo_info}"
    repo_name=$( echo $repo_info | cut -d ":" -f 1 )
    repo_branch=$( echo $repo_info | cut -d ":" -f 2 )
    source_path=$( echo $repo_info | cut -d ":" -f 3 )
    target_path=$( echo $repo_info | cut -d ":" -f 4 )
    repo_url="${GITHUB_URL}/${MANIFEST_ORG}/${repo_name}.git"
    rm -rf ./.${repo_name}
    git clone --depth 1 --branch ${repo_branch} ${repo_url} ./.${repo_name}
    mkdir -p ./odh-manifests/${target_path}
    cp -rf ./.${repo_name}/${source_path}/* ./odh-manifests/${target_path}
    rm -rf ./.${repo_name}
done