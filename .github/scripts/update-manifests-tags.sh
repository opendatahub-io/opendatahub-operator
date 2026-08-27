#!/bin/bash

set -euo pipefail

update_tags(){
    local component=$1
    local value=$2
    yq -i ".components.\"$component\".odh.ref = \"$value\"" manifests-config.yaml
    yq -i ".components.\"$component\".rhoai.ref = \"$value\"" manifests-config.yaml
}

update_org(){
    local component=$1
    local value=$2
    
    local current_repo_odh
    current_repo_odh=$(yq ".components.\"$component\".odh.repo" manifests-config.yaml)
    if [[ -n "$current_repo_odh" && "$current_repo_odh" != "null" ]]; then
        local repo_name_odh="${current_repo_odh#*/}"
        yq -i ".components.\"$component\".odh.repo = \"$value/$repo_name_odh\"" manifests-config.yaml
    fi

    local current_repo_rhoai
    current_repo_rhoai=$(yq ".components.\"$component\".rhoai.repo" manifests-config.yaml)
    if [[ -n "$current_repo_rhoai" && "$current_repo_rhoai" != "null" ]]; then
        local repo_name_rhoai="${current_repo_rhoai#*/}"
        yq -i ".components.\"$component\".rhoai.repo = \"$value/$repo_name_rhoai\"" manifests-config.yaml
    fi
}

spec_prefix=component_spec_
org_prefix=component_org_

echo "Updating component branches/tags in manifests-config.yaml..."
env | while IFS="=" read varname value; do
    [[ $varname =~ $spec_prefix ]] || continue
    component=${varname#${spec_prefix}}
    component=${component//_/-}
    
    # Map back to manifests-config.yaml keys
    if [[ "$component" == "odh-notebook-controller" ]]; then
        component="workbenches/odh-notebook-controller"
    elif [[ "$component" == "kf-notebook-controller" ]]; then
        component="workbenches/kf-notebook-controller"
    elif [[ "$component" == "notebooks" ]]; then
        component="workbenches/notebooks"
    fi

    echo "  Updating branch/tag for $component to: $value"
    update_tags "$component" "$value"
done

echo "Updating component repository organizations in manifests-config.yaml..."
env | while IFS="=" read varname value; do
    [[ $varname =~ $org_prefix ]] || continue
    component=${varname#${org_prefix}}
    component=${component//_/-}
    
    # Map back to manifests-config.yaml keys
    if [[ "$component" == "odh-notebook-controller" ]]; then
        component="workbenches/odh-notebook-controller"
    elif [[ "$component" == "kf-notebook-controller" ]]; then
        component="workbenches/kf-notebook-controller"
    elif [[ "$component" == "notebooks" ]]; then
        component="workbenches/notebooks"
    fi

    echo "  Updating organization for $component to: $value"
    update_org "$component" "$value"
done
