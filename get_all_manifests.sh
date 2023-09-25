#!/bin/bash
set -e

# component: dsp, kserve, dashbaord, cf/ray. in the format of "repo-name:branch-name:source-folder:target-folder"
# TODO: workbench, modelmesh, monitoring, etc
REPO_LIST=(
    "data-science-pipelines-operator:main:config:data-science-pipelines-operator"
    "odh-dashboard:incubation:manifests:odh-dashboard"
    "notebooks:main:manifests:notebook-images"
    "kubeflow:v1.7-branch:components/notebook-controller/config:odh-notebook-controller/kf-notebook-controller"
    "kubeflow:v1.7-branch:components/odh-notebook-controller/config:odh-notebook-controller/odh-notebook-controller"
)

# pre-cleanup local env
rm -fr ./odh-manifests/* ./.odh-manifests-tmp/

GITHUB_URL="https://github.com/"
# update to use different git repo
MANIFEST_ORG="opendatahub-io"

MANIFEST_RELEASE="master"
MANIFESTS_TARBALL_URL="${GITHUB_URL}/${MANIFEST_ORG}/odh-manifests/tarball/${MANIFEST_RELEASE}"
mkdir -p ./.odh-manifests-tmp/ ./odh-manifests/
wget -q -c ${MANIFESTS_TARBALL_URL} -O - | tar -zxv -C ./.odh-manifests-tmp/ --strip-components 1 > /dev/null
cp -r ./.odh-manifests-tmp/model-mesh/ ./odh-manifests
cp -r ./.odh-manifests-tmp/odh-model-controller/ ./odh-manifests
cp -r ./.odh-manifests-tmp/modelmesh-monitoring/ ./odh-manifests
cp -r ./.odh-manifests-tmp/prometheus ./odh-manifests
cp -r ./.odh-manifests-tmp/trustyai-service-operator ./odh-manifests
cp -r ./.odh-manifests-tmp/odh-common ./odh-manifests
rm -rf ${MANIFEST_RELEASE}.tar.gz ./.odh-manifests-tmp/

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
