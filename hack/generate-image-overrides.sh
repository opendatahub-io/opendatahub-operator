#!/usr/bin/env bash
# Generate RELATED_IMAGE_* override env file from manifests-config.yaml.
# Reads pinned digests from the imageOverrides section and outputs base@digest entries.
# imageOverrides is keyed by RELATED_IMAGE_* env var name.
# Output: opt/related-images-override.env
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
CONFIG_FILE="${ROOT_DIR}/manifests-config.yaml"
OUTPUT_FILE="${ROOT_DIR}/opt/related-images-override.env"
YQ="${YQ:-yq}"

if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "ERROR: $CONFIG_FILE not found"
    exit 1
fi

platform="odh"
if [[ "${ODH_PLATFORM_TYPE:-OpenDataHub}" != "OpenDataHub" ]]; then
    platform="rhoai"
fi

env_names=$("$YQ" eval ".imageOverrides | keys | .[]" "$CONFIG_FILE")
if [[ -z "$env_names" ]]; then
    echo "No image overrides defined"
    exit 0
fi

mkdir -p "$(dirname "$OUTPUT_FILE")"
: > "$OUTPUT_FILE"

DIGEST_PATTERN='^sha256:[a-f0-9]{64}$'

while IFS= read -r env_name; do
    local_base=$("$YQ" eval ".imageOverrides.\"${env_name}\".${platform}.base // \"\"" "$CONFIG_FILE")
    local_digest=$("$YQ" eval ".imageOverrides.\"${env_name}\".${platform}.digest // \"\"" "$CONFIG_FILE")

    if [[ -z "$local_digest" ]]; then
        echo "WARNING: no digest for ${env_name} (${platform}), skipping (run 'make resolve-image-digests')"
        continue
    fi

    if ! [[ "$local_digest" =~ $DIGEST_PATTERN ]]; then
        echo "WARNING: invalid digest for ${env_name} (${platform}): ${local_digest}, skipping"
        continue
    fi

    if [[ -z "$local_base" ]]; then
        echo "WARNING: no base for ${env_name} (${platform}), skipping"
        continue
    fi

    if [[ "$local_base" =~ [\'\"\;\$\`\|] ]]; then
        echo "WARNING: base contains unsafe characters for ${env_name} (${platform}): ${local_base}, skipping"
        continue
    fi

    echo "${env_name}=${local_base}@${local_digest}" >> "$OUTPUT_FILE"
    echo "  Override: ${env_name}=${local_base}@${local_digest}"
done <<< "$env_names"

echo "Image overrides written to $OUTPUT_FILE"
