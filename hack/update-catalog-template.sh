#!/bin/bash

set -euo pipefail

CATALOG_TEMPLATE=${1:-config/catalog/fbc-basic-template.yaml}
BUNDLE_IMGS=${2:-}
YQ=${3:-yq}

if [[ -z "$BUNDLE_IMGS" ]]; then
    echo "Error: BUNDLE_IMGS is required. Please provide at least one image in the format 'image:tag' or 'image:tag,image:tag'" >&2
    exit 1
fi

if ! [[ "$BUNDLE_IMGS" =~ ^[^:]+:[^:,]+([,][^:]+:[^:,]+)*$ ]]; then
    echo "Error: Invalid BUNDLE_IMGS format. Expected format: 'image:tag' or 'image:tag,image:tag,...'" >&2
    echo "Got: $BUNDLE_IMGS" >&2
    exit 1
fi

YQ_VERSION=$($YQ --version | grep -o 'version.*' | cut -d' ' -f2)
if [[ ! "$YQ_VERSION" =~ ^v?4\. ]]; then
    echo "Error: YQ version 4.x.x is required. Found version: $YQ_VERSION" >&2
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
IFS=',' read -ra images <<< "$BUNDLE_IMGS"
for img in "${images[@]}"; do
    prev_version=$(add_bundle "$package_name" "$img" "$prev_version")
done

echo "Catalog template updated successfully!"
