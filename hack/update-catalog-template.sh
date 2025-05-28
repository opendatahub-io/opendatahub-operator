#!/usr/bin/env bash

set -euo pipefail

CATALOG_TEMPLATE=${1:-config/catalog/fbc-basic-template.yaml}
BUNDLE_IMGS=${2:-}
YQ=${3:-yq}

if ! command -v "$YQ" &> /dev/null; then
    echo "Error: $YQ not found. Please ensure yq is installed." >&2
    exit 1
fi

package_name="opendatahub-operator"

function add_bundle() {
    local package_name=$1 img=$2 prev_version=$3
    local version bundle_name
    
    version=$(echo "$img" | cut -d':' -f2)
    bundle_name="${package_name}.${version}"

    $YQ -i e "
        select(.schema == \"olm.template.basic\").entries += [{
            \"schema\": \"olm.bundle\",
            \"image\": \"$img\"
        }]
    " "$CATALOG_TEMPLATE"

    if [[ -n "$prev_version" ]]; then
        $YQ -i e "
            select(.schema == \"olm.template.basic\").entries[] |=
                select(.schema == \"olm.channel\" and .name == \"fast\").entries += [{
                    \"name\": \"$bundle_name\",
                    \"replaces\": \"$package_name.$prev_version\"
                }]
        " "$CATALOG_TEMPLATE"
    else
        $YQ -i e "
            select(.schema == \"olm.template.basic\").entries[] |=
                select(.schema == \"olm.channel\" and .name == \"fast\").entries += [{
                    \"name\": \"$bundle_name\"
                }]
        " "$CATALOG_TEMPLATE"
    fi

    echo "$version"
}

echo "Package Name: $package_name"

prev_version=""
IFS=',' read -r -a images <<< "$BUNDLE_IMGS"
for img in "${images[@]}"; do
    prev_version=$(add_bundle "$package_name" "$img" "$prev_version")
done

echo "Catalog template updated successfully!"
