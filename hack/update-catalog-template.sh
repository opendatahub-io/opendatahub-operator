#!/usr/bin/env bash

set -euo pipefail

CATALOG_TEMPLATE=${1:-config/catalog/fbc-basic-template.yaml}
BUNDLE_IMGS=${2:-}
YQ=${3:-yq}

if ! command -v "$YQ" &> /dev/null; then
    echo "Error: $YQ not found. Please ensure yq is installed." >&2
    exit 1
fi

function extract_package_name() {
    local name
    name=$($YQ e '.entries[] | select(.schema == "olm.package") | .name' "$CATALOG_TEMPLATE")
    if [[ -z "$name" ]]; then
        echo "Error: Could not extract package name from catalog template" >&2
        exit 1
    fi
    echo "$name"
}

function create_fast_channel() {
    local package_name=$1
    $YQ -i e "
        select(.schema == \"olm.template.basic\").entries += [{
            \"schema\": \"olm.channel\",
            \"package\": \"$package_name\",
            \"name\": \"fast\",
            \"entries\": []
        }]
    " "$CATALOG_TEMPLATE"
}

function add_bundle() {
    local package_name=$1 img=$2 prev_version=$3
    local version bundle_name
    
    version=$(echo "$img" | sed -E 's/.*:v?([0-9]+\.[0-9]+\.[0-9]+)$/\1/')
    bundle_name="${package_name}.v${version}"

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
                    \"replaces\": \"$package_name.v$prev_version\"
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

package_name=$(extract_package_name)
echo "Package Name: $package_name"

if [[ -z "$($YQ e '.entries[] | select(.schema == "olm.channel" and .name == "fast")' "$CATALOG_TEMPLATE")" ]]; then
    create_fast_channel "$package_name"
fi

prev_version=""
IFS=',' read -r -a images <<< "$BUNDLE_IMGS"
for img in "${images[@]}"; do
    prev_version=$(add_bundle "$package_name" "$img" "$prev_version")
done

echo "Catalog template updated successfully!"
