#!/bin/bash
# Validates that all RELATED_IMAGE_* names used in the operator source code
# are present in ODH-Build-Config and/or RHOAI-Build-Config bundle-patch.yaml
# and additional-images-patch.yaml files.
#
# Environment variables:
#   RHOAI_BUILD_CONFIG_BRANCH - Branch in RHOAI-Build-Config repo (e.g. rhoai-3.3)
#   ODH_BUILD_CONFIG_BRANCH   - Branch in ODH-Build-Config repo (default: main)
#   DEBUG                     - Set to "true" to keep intermediate files for inspection

set -euo pipefail

ODH_BUILD_CONFIG_BRANCH="${ODH_BUILD_CONFIG_BRANCH:-main}"
RHOAI_BUILD_CONFIG_BRANCH="${RHOAI_BUILD_CONFIG_BRANCH:?RHOAI_BUILD_CONFIG_BRANCH is required}"

# Validate branch names contain only safe characters
validate_branch_name() {
    local branch="$1"
    local var_name="$2"
    if ! echo "$branch" | grep -qE '^[a-zA-Z0-9._/-]+$'; then
        echo "ERROR: ${var_name} contains invalid characters: ${branch}" >&2
        exit 1
    fi
}
validate_branch_name "$ODH_BUILD_CONFIG_BRANCH" "ODH_BUILD_CONFIG_BRANCH"
validate_branch_name "$RHOAI_BUILD_CONFIG_BRANCH" "RHOAI_BUILD_CONFIG_BRANCH"

ODH_REPO="opendatahub-io/ODH-Build-Config"
RHOAI_REPO="red-hat-data-services/RHOAI-Build-Config"

ODH_BASE_URL="https://raw.githubusercontent.com/${ODH_REPO}/${ODH_BUILD_CONFIG_BRANCH}"
RHOAI_BASE_URL="https://raw.githubusercontent.com/${RHOAI_REPO}/${RHOAI_BUILD_CONFIG_BRANCH}"

DEBUG="${DEBUG:-false}"

TMPDIR="${TMPDIR:-/tmp}"
WORKDIR=$(mktemp -d "${TMPDIR}/validate-related-images.XXXXXX")
if [ "$DEBUG" = "true" ]; then
    echo "DEBUG: Working directory: ${WORKDIR} (will NOT be deleted)"
else
    trap 'rm -rf "$WORKDIR"' EXIT
fi

echo "Validating RELATED_IMAGE_* references against build configs..."
echo "  ODH-Build-Config branch:  ${ODH_BUILD_CONFIG_BRANCH}"
echo "  RHOAI-Build-Config branch: ${RHOAI_BUILD_CONFIG_BRANCH}"
echo ""

# Step 1: Extract RELATED_IMAGE_* names from internal/ (excluding test files)
# Collect all RELATED_IMAGE_* strings, then subtract Go map keys
# (strings before ":") to keep only env var references.

# All RELATED_IMAGE_* strings
grep -roh 'RELATED_IMAGE_[A-Z0-9_]\+' internal/ \
    --include='*.go' --exclude='*_test.go' \
    | sort -u > "$WORKDIR/all-refs.txt"

# Map keys only: "RELATED_IMAGE_KEY": (before colon in Go map literals)
grep -roh '"RELATED_IMAGE_[A-Z0-9_]*"[[:space:]]*:' internal/ \
    --include='*.go' --exclude='*_test.go' 2>/dev/null \
    | grep -oh 'RELATED_IMAGE_[A-Z0-9_]\+' \
    | sort -u > "$WORKDIR/map-keys.txt"

# Subtract map keys that are NOT also used as values elsewhere
comm -23 "$WORKDIR/all-refs.txt" "$WORKDIR/map-keys.txt" > "$WORKDIR/operator-images.txt"

OPERATOR_COUNT=$(wc -l < "$WORKDIR/operator-images.txt" | tr -d ' ')
echo "Found ${OPERATOR_COUNT} unique RELATED_IMAGE_* names in internal/"

# Step 2: Fetch build config files and extract RELATED_IMAGE names
fetch_and_extract() {
    local url="$1"
    local output="$2"
    local label="$3"

    if ! curl -sfL --max-filesize 10485760 --connect-timeout 10 --max-time 30 \
            "$url" -o "$WORKDIR/fetched.yaml" 2>/dev/null; then
        echo "WARNING: Failed to fetch ${label}: ${url}"
        return 1
    fi
    grep -oh 'RELATED_IMAGE_[A-Z0-9_]\+' "$WORKDIR/fetched.yaml" >> "$output" 2>/dev/null || true
    rm -f "$WORKDIR/fetched.yaml"
}

# Fetch RELATED_IMAGE names from a build config repo.
# Args: label, base_url, output_file
fetch_repo_images() {
    local label="$1"
    local base_url="$2"
    local output="$3"
    local fetch_failed=0

    touch "$output"
    echo ""
    echo "Fetching ${label}..."
    for file in bundle-patch.yaml additional-images-patch.yaml; do
        if ! fetch_and_extract "${base_url}/bundle/${file}" "$output" "${label} ${file}"; then
            fetch_failed=1
        fi
    done

    if [ "$fetch_failed" -eq 1 ] && [ ! -s "$output" ]; then
        echo "ERROR: Could not fetch any files from ${label}"
        exit 1
    fi
    sort -u -o "$output" "$output"
    local count
    count=$(wc -l < "$output" | tr -d ' ')
    echo "  Found ${count} RELATED_IMAGE_* names in ${label}"
}

ODH_IMAGES="$WORKDIR/odh-images.txt"
RHOAI_IMAGES="$WORKDIR/rhoai-images.txt"

fetch_repo_images "ODH-Build-Config (${ODH_BUILD_CONFIG_BRANCH})" "$ODH_BASE_URL" "$ODH_IMAGES"
fetch_repo_images "RHOAI-Build-Config (${RHOAI_BUILD_CONFIG_BRANCH})" "$RHOAI_BASE_URL" "$RHOAI_IMAGES"

# Step 3: Find operator images missing from any build config
# An image must be present in ALL build configs, not just one.
REPO_LABELS=("ODH-Build-Config (${ODH_BUILD_CONFIG_BRANCH})" "RHOAI-Build-Config (${RHOAI_BUILD_CONFIG_BRANCH})")
REPO_FILES=("$ODH_IMAGES" "$RHOAI_IMAGES")

HAS_ERRORS=false

echo ""
while IFS= read -r img; do
    missing_from=""
    present_in=""
    for i in "${!REPO_LABELS[@]}"; do
        if grep -qx "$img" "${REPO_FILES[$i]}"; then
            present_in="${present_in:+${present_in}, }${REPO_LABELS[$i]}"
        else
            missing_from="${missing_from:+${missing_from}, }${REPO_LABELS[$i]}"
        fi
    done

    [ -z "$missing_from" ] && continue

    # Print header on first error
    if [ "$HAS_ERRORS" = false ]; then
        echo "ERROR: RELATED_IMAGE_* names missing from build configs:"
        echo ""
        HAS_ERRORS=true
    fi

    echo "  ${img}"
    grep -rn "$img" internal/ --include='*.go' --exclude='*_test.go' | head -3 | while IFS= read -r line; do
        echo "    Source: ${line%%:*}:$(echo "$line" | cut -d: -f2)"
    done
    echo "    Missing from: ${missing_from}"
    [ -n "$present_in" ] && echo "    Present in:   ${present_in}"
    echo ""
done < "$WORKDIR/operator-images.txt"

if [ "$HAS_ERRORS" = true ]; then
    # Summary: list images grouped by which repo they're missing from
    echo "---"
    echo "Summary:"
    for i in "${!REPO_LABELS[@]}"; do
        missing_list=$(comm -23 "$WORKDIR/operator-images.txt" "${REPO_FILES[$i]}")
        if [ -n "$missing_list" ]; then
            echo ""
            echo "  Missing from ${REPO_LABELS[$i]}:"
            echo "$missing_list" | while IFS= read -r img; do
                echo "    - ${img}"
            done
        fi
    done

    echo ""
    echo "Please ensure these images are added to the build config repos before merging."
    echo "See: https://github.com/${ODH_REPO} and https://github.com/${RHOAI_REPO}"
    exit 1
fi

echo "All RELATED_IMAGE_* references are present in all build configs."
