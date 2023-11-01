#!/bin/bash
set -e

# component: dsp, kserve, dashbaord, cf/ray. in the format of "repo-name:branch-name:source-folder:target-folder"
# TODO: workbench, modelmesh, monitoring, etc
REPO_LIST=(
    "data-science-pipelines-operator:v1.6.0:config:data-science-pipelines-operator"
    "odh-dashboard:v2.17.0-incubation:manifests:odh-dashboard"
    "notebooks:v1.12.0:manifests:notebook-images"
    "trustyai-service-operator:v1.11.1:config:trustyai-service-operator"
    "kubeflow:v1.7.0-5:components/notebook-controller/config:odh-notebook-controller/kf-notebook-controller"
    "kubeflow:v1.7.0-5:components/odh-notebook-controller/config:odh-notebook-controller/odh-notebook-controller"
    "odh-model-controller:v0.11.1.0:config:model-mesh/odh-model-controller"
    "modelmesh-serving:v0.11.1.0:config:model-mesh/modelmesh"
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
cp -r ./.odh-manifests-tmp/prometheus ./odh-manifests
cp -r ./.odh-manifests-tmp/odh-common ./odh-manifests
# This is required, adding base dir under components. Overlays are not working with KfDef
mkdir -p ./odh-manifests/odh-notebook-controller/base
cp -r ./.odh-manifests-tmp/odh-notebook-controller/base/ ./odh-manifests/odh-notebook-controller
cp -r ./pkg/manifests/model-mesh/ ./odh-manifests/model-mesh
rm -rf ${MANIFEST_RELEASE}.tar.gz ./.odh-manifests-tmp/

for repo_info in ${REPO_LIST[@]}; do
    echo "Git clone below repo ${repo_info}"
    repo_name=$( echo $repo_info | cut -d ":" -f 1 )
    repo_branch=$( echo $repo_info | cut -d ":" -f 2 )
    source_path=$( echo $repo_info | cut -d ":" -f 3 )
    target_path=$( echo $repo_info | cut -d ":" -f 4 )

    if [ ${repo_name} == "trustyai-service-operator" ]
    then
       repo_url="${GITHUB_URL}/trustyai-explainability/${repo_name}.git"
    else
       repo_url="${GITHUB_URL}/${MANIFEST_ORG}/${repo_name}.git"
    fi

    rm -rf ./.${repo_name}
    git clone --depth 1 --branch ${repo_branch} ${repo_url} ./.${repo_name}
    mkdir -p ./odh-manifests/${target_path}
    cp -rf ./.${repo_name}/${source_path}/* ./odh-manifests/${target_path}
    rm -rf ./.${repo_name}
done

