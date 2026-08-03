#!/usr/bin/env bash
# Resolve image digests and update manifests-config.yaml.
# imageOverrides is keyed by RELATED_IMAGE_* env var name.
# Priority: 1. tagTemplate (skopeo) → 2. Build-Config CSV → 3. params.env
# Requires: curl, yq. Optional: skopeo (for tagTemplate fallback).
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
CONFIG_FILE="${ROOT_DIR}/manifests-config.yaml"
MANIFESTS_DIR="${ROOT_DIR}/opt/manifests"
YQ="${YQ:-yq}"

ALLOWED_BUILD_CONFIG_REPOS="opendatahub-io/ODH-Build-Config red-hat-data-services/RHOAI-Build-Config"
BUILD_CONFIG_REPO="${ODH_BUILD_CONFIG_REPO:-opendatahub-io/ODH-Build-Config}"
BUILD_CONFIG_BRANCH="${ODH_BUILD_CONFIG_BRANCH:-main}"

if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "ERROR: $CONFIG_FILE not found"
    exit 1
fi

# Validate Build-Config repo against allowlist
if ! echo "$ALLOWED_BUILD_CONFIG_REPOS" | grep -qw "$BUILD_CONFIG_REPO"; then
    echo "ERROR: BUILD_CONFIG_REPO '$BUILD_CONFIG_REPO' not in allowlist: $ALLOWED_BUILD_CONFIG_REPOS"
    exit 1
fi

# Step 1: Fetch Build-Config CSV
CSV_URL="https://raw.githubusercontent.com/${BUILD_CONFIG_REPO}/${BUILD_CONFIG_BRANCH}/bundle/manifests/rhods-operator.clusterserviceversion.yaml"
echo "Fetching Build-Config CSV from ${BUILD_CONFIG_REPO}:${BUILD_CONFIG_BRANCH}..."

csv_file=$(mktemp "${TMPDIR:-/tmp}/resolve-digests.XXXXXX")
trap 'rm -f "$csv_file"' EXIT
curl -sfL --max-time 30 "$CSV_URL" -o "$csv_file" || {
    echo "WARNING: failed to fetch Build-Config CSV, will use fallbacks only"
    csv_file=""
}

# Step 2: Build lookup map from CSV
DIGEST_PATTERN='@sha256:[a-f0-9]{64}$'
declare -A BUILD_CONFIG_IMAGES=()
if [[ -n "$csv_file" && -f "$csv_file" ]]; then
    yq_output=$("$YQ" eval '.spec.install.spec.deployments[0].spec.template.spec.containers[0].env[] | [.name, .value] | @tsv' "$csv_file") || {
        echo "ERROR: failed to parse Build-Config CSV with yq"
        exit 1
    }
    while IFS=$'\t' read -r name value; do
        [[ "$name" == RELATED_IMAGE_* ]] || continue
        if ! [[ "$value" =~ $DIGEST_PATTERN ]]; then
            echo "WARNING: skipping $name — value does not contain valid @sha256 digest: $value"
            continue
        fi
        BUILD_CONFIG_IMAGES["$name"]="$value"
    done <<< "$yq_output"
    echo "Loaded ${#BUILD_CONFIG_IMAGES[@]} RELATED_IMAGE entries from Build-Config"
fi

resolve_via_skopeo() {
    local image_ref=$1
    if ! command -v skopeo &>/dev/null; then
        echo "  WARNING: skopeo not found, cannot resolve $image_ref" >&2
        return 1
    fi
    echo "  Resolving via skopeo: $image_ref ..." >&2
    skopeo --command-timeout 60s inspect --no-tags --override-arch amd64 --override-os linux --format '{{.Digest}}' "docker://${image_ref}" 2>/dev/null || {
        echo "  WARNING: skopeo failed for $image_ref" >&2
        return 1
    }
}

# Helper: write base+digest for a platform using sed (avoids yq corrupting tagTemplate).
# Finds the RELATED_IMAGE block, then the platform sub-block, and updates base/digest lines.
write_base_digest() {
    local env_name=$1
    local platform=$2
    local new_base=$3
    local new_digest=$4

    # Use awk to find the correct block and update base/digest
    awk -v env="$env_name" -v plat="$platform" -v base="$new_base" -v digest="$new_digest" '
    BEGIN { in_env=0; in_plat=0; env_indent=-1; plat_indent=-1 }
    {
        # Detect entry block start: "  RELATED_IMAGE_...:""
        if ($0 ~ "^[[:space:]]*" env ":") {
            in_env=1; in_plat=0
            match($0, /^[[:space:]]*/); env_indent=RLENGTH
        }
        # Detect we left the entry block
        else if (in_env && match($0, /^[[:space:]]*[A-Z]/) && RLENGTH-1 <= env_indent && $0 !~ /^[[:space:]]*#/) {
            in_env=0; in_plat=0
        }
        # Detect platform sub-block
        if (in_env && $0 ~ "^[[:space:]]*" plat ":") {
            in_plat=1
            match($0, /^[[:space:]]*/); plat_indent=RLENGTH
        }
        # Detect we left the platform sub-block
        else if (in_plat && match($0, /^[[:space:]]*[a-z]/) && RLENGTH-1 <= plat_indent) {
            in_plat=0
        }
        # Replace base line within platform block
        if (in_plat && $0 ~ /^[[:space:]]*base:/) {
            sub(/base:.*/, "base: " base)
        }
        # Replace digest line within platform block
        if (in_plat && $0 ~ /^[[:space:]]*digest:/) {
            sub(/digest:.*/, "digest: \"" digest "\"")
        }
        print
    }' "$CONFIG_FILE" > "${CONFIG_FILE}.tmp" && mv "${CONFIG_FILE}.tmp" "$CONFIG_FILE"
}

# Step 3: Resolve each entry
resolve_entry() {
    local env_name=$1
    local local_path=".imageOverrides.\"${env_name}\""
    local component tag_template params_env_key base_default
    component=$($YQ eval "${local_path}.component // \"\"" "$CONFIG_FILE")
    tag_template=$($YQ eval "${local_path}.tagTemplate // \"\"" "$CONFIG_FILE")
    params_env_key=$($YQ eval "${local_path}.paramsEnvKey // \"\"" "$CONFIG_FILE")
    # Read base from top level (default for tagTemplate entries)
    base_default=$($YQ eval "${local_path}.base // \"\"" "$CONFIG_FILE")

    for platform in odh rhoai; do
        # Check if platform exists for this component
        local platform_exists=""
        for section in components ccmCharts componentCharts; do
            platform_exists=$($YQ eval ".${section}.\"${component}\".${platform}.ref // \"\"" "$CONFIG_FILE")
            [[ -n "$platform_exists" && "$platform_exists" != "null" ]] && break
        done
        [[ -z "$platform_exists" || "$platform_exists" == "null" ]] && continue

        # Skip pinned digests
        local pinned
        pinned=$("$YQ" eval "${local_path}.${platform}.pinned // false" "$CONFIG_FILE")
        if [[ "$pinned" == "true" ]]; then
            echo "  $env_name ($platform): pinned, skipping"
            continue
        fi

        # Check shaFrom — copy from source platform
        local sha_from
        sha_from=$($YQ eval "${local_path}.${platform}.shaFrom // \"\"" "$CONFIG_FILE")
        if [[ -n "$sha_from" ]]; then
            local source_base source_digest
            source_base=$($YQ eval "${local_path}.${sha_from}.base // \"\"" "$CONFIG_FILE")
            source_digest=$($YQ eval "${local_path}.${sha_from}.digest // \"\"" "$CONFIG_FILE")
            if [[ -n "$source_digest" ]]; then
                write_base_digest "$env_name" "$platform" "$source_base" "$source_digest"
                echo "  $env_name ($platform): copied from $sha_from [shaFrom]"
                continue
            fi
        fi

        # Priority 1: tagTemplate → skopeo
        # Use top-level base, or per-platform base as fallback
        local effective_base="${base_default}"
        if [[ -z "$effective_base" ]]; then
            effective_base=$($YQ eval "${local_path}.${platform}.base // \"\"" "$CONFIG_FILE")
        fi
        if [[ -n "$effective_base" && -n "$tag_template" ]]; then
            local ref=""
            for section in components ccmCharts componentCharts; do
                ref=$($YQ eval ".${section}.\"${component}\".${platform}.ref // \"\"" "$CONFIG_FILE")
                [[ -n "$ref" && "$ref" != "null" ]] && break
            done

            local sha=""
            if [[ "$ref" =~ @([a-f0-9]{7,40})$ ]]; then
                sha="${BASH_REMATCH[1]}"
            fi

            if [[ -n "$sha" ]]; then
                local short_sha="${sha:0:7}"
                local resolved_tag="${tag_template//\{SHA\}/$sha}"
                resolved_tag="${resolved_tag//\{SHORT_SHA\}/$short_sha}"
                local image_ref="${effective_base}:${resolved_tag}"

                local digest
                digest=$(resolve_via_skopeo "$image_ref") || true
                if [[ -n "$digest" ]]; then
                    write_base_digest "$env_name" "$platform" "$effective_base" "$digest"
                    echo "  $env_name ($platform): ${effective_base}@${digest} [tagTemplate: ${resolved_tag}, component: ${component}, sha: ${sha:0:12}]"
                    continue
                fi
                echo "  $env_name ($platform): skopeo failed for ${image_ref} (component: ${component}, sha: ${sha:0:12}), falling through..."
            fi
        fi

        # Priority 2: Build-Config CSV
        local bc_value="${BUILD_CONFIG_IMAGES[$env_name]:-}"
        if [[ -n "$bc_value" ]]; then
            local bc_base="${bc_value%%@*}"
            local bc_digest="${bc_value#*@}"
            write_base_digest "$env_name" "$platform" "$bc_base" "$bc_digest"
            echo "  $env_name ($platform): ${bc_base}@${bc_digest} [Build-Config]"
            continue
        fi

        # Priority 3: params.env
        if [[ -n "$params_env_key" && -n "$component" ]]; then
            local params_file="${MANIFESTS_DIR}/${component}/params.env"
            if [[ -f "$params_file" ]]; then
                local image_ref
                image_ref=$(grep "^${params_env_key}=" "$params_file" | head -1 | cut -d= -f2-)
                if [[ -n "$image_ref" ]]; then
                    if [[ "$image_ref" == *"@sha256:"* ]]; then
                        local p_base="${image_ref%%@*}"
                        local p_digest="${image_ref#*@}"
                        write_base_digest "$env_name" "$platform" "$p_base" "$p_digest"
                        echo "  $env_name ($platform): ${p_base}@${p_digest} [params.env]"
                        continue
                    fi
                    local digest
                    digest=$(resolve_via_skopeo "$image_ref") || true
                    if [[ -n "$digest" ]]; then
                        local p_base="${image_ref%%:*}"
                        write_base_digest "$env_name" "$platform" "$p_base" "$digest"
                        echo "  $env_name ($platform): ${p_base}@${digest} [params.env+skopeo]"
                        continue
                    fi
                fi
            fi
        fi

        echo "  WARNING: $env_name ($platform): no source found"
    done
}

env_names=$($YQ eval ".imageOverrides | keys | .[]" "$CONFIG_FILE" 2>/dev/null || true)
[[ -z "$env_names" ]] && { echo "No image overrides defined"; exit 0; }

while IFS= read -r env_name; do
    resolve_entry "$env_name"
done <<< "$env_names"




echo "Digests updated in $CONFIG_FILE"
