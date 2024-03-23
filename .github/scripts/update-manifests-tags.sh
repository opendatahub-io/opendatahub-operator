#!/bin/bash

set -euo pipefail

update_tags(){
MANIFEST_STR=$(cat get_all_manifests.sh | grep $1 | sed 's/ //g')
    readarray -d ":" -t STR_ARR <<< "$MANIFEST_STR"
    RES=""
    for i in "${!STR_ARR[@]}"; do
        if [ $i == 2 ]; then
            RES+=$2":"
        else
            RES+=${STR_ARR[$i]}":"
        fi
    done
    echo "${RES::-2}"
    sed -i -r "s|.*$1.*|    ${RES::-2}|" get_all_manifests.sh
}

declare -A COMPONENT_VERSION_MAP=(
    ["\"codeflare\""]=$1
    ["\"ray\""]=$2
    ["\"kueue\""]=$3
    ["\"data-science-pipelines-operator\""]=$4
    ["\"odh-dashboard\""]=$5
    ["\"kf-notebook-controller\""]=$6
    ["\"odh-notebook-controller\""]=$7
    ["\"notebooks\""]=$8
    ["\"trustyai\""]=$9
    ["\"model-mesh\""]=$10
    ["\"odh-model-controller\""]=$11
    ["\"kserve\""]=$12
    ["\"modelregistry\""]=$13
)

for key in ${!COMPONENT_VERSION_MAP[@]}; do
    update_tags ${key} ${COMPONENT_VERSION_MAP[${key}]}
done