#!/usr/bin/env bash

set -euo pipefail

yq=$1
destination_dir=$2
temporary_file=$(mktemp "${destination_dir}/.bundle-component-crd.XXXXXX")
trap 'rm -f "$temporary_file"' EXIT

# Use pinned module schemas so OLM installs CRDs before DSC reconciliation.
# Keep this list in sync with the GVKs in internal/controller/modules/.
while read -r plural source_dir; do
    crd_name="${plural}.components.platform.opendatahub.io"
    if [[ ! -d "$source_dir" ]]; then
        echo "Missing component manifests: $source_dir" >&2
        exit 1
    fi

    found=false
    while IFS= read -r source_file; do
        if ! grep -qF "$crd_name" "$source_file"; then
            continue
        fi

        CRD_NAME="$crd_name" "$yq" eval \
            'select(.apiVersion == "apiextensions.k8s.io/v1" and .kind == "CustomResourceDefinition" and .metadata.name == env(CRD_NAME))' \
            "$source_file" > "$temporary_file"
        if [[ -s "$temporary_file" ]]; then
            cp "$temporary_file" "${destination_dir}/${crd_name}.yaml"
            found=true
            break
        fi
    done < <(find "$source_dir" -type f \( -name '*.yaml' -o -name '*.yml' \) | sort)

    if [[ "$found" != true ]]; then
        echo "Missing component CRD $crd_name in $source_dir" >&2
        exit 1
    fi
done <<'CRDS'
aigateways opt/manifests/aigateway/crd/bases
aihubs opt/manifests/aihub/overlays/aihub
aipipelines opt/manifests/datasciencepipelines/crd/bases
dashboards opt/charts/dashboard-operator/crds
feastoperators opt/charts/data/crds
kserves opt/manifests/kserve-module-operator/crd
mcplifecycleoperators opt/manifests/mcplifecycleoperator
mlflowoperators opt/manifests/mlflowoperator/crd/bases
ogxs opt/manifests/ogx/crd/bases
rays opt/manifests/ray/crd/bases
sparkoperators opt/manifests/sparkoperator
trainers opt/manifests/trainer/crd/bases
trustyais opt/manifests/trustyai/crd/bases
workbenches opt/charts/workbenches/crd
CRDS
