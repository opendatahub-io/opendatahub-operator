#!/bin/bash

set -euo pipefail

update_tags(){
    sed -i -r "/\"(.*\/)*$1\"/s|([^:]*):([^:]*):[^:]*:(.*)|\1:\2:$2:\3|" get_all_manifests.sh
}

update_org(){
    sed -i -r "/\"(.*\/)*$1\"/s|=\"([^:]*):([^:]*):([^:]*):(.*)\"|=\"$2:\2:\3:\4\"|" get_all_manifests.sh
}

spec_prefix=component_spec_
org_prefix=component_org_

echo "Updating component branches/tags..."
env | while IFS="=" read varname value; do
    [[ $varname =~ $spec_prefix ]] || continue
    component=${varname#${spec_prefix}}
    component=${component//_/-}
    echo "  Updating branch/tag for $(basename "$component") to: $value"
    update_tags "$(basename "$component")" "$value"
done

echo "Updating component repository organizations..."
env | while IFS="=" read varname value; do
    [[ $varname =~ $org_prefix ]] || continue
    component=${varname#${org_prefix}}
    component=${component//_/-}
    echo "  Updating organization for $(basename "$component") to: $value"
    update_org "$(basename "$component")" "$value"
done

