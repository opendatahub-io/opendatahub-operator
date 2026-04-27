#!/bin/bash
# Validates RELATED_IMAGE_* references per-platform:
#   params.env (overlay) → map key → RELATED_IMAGE_* → Build-Config
#
# Environment variables:
#   RHOAI_BUILD_CONFIG_BRANCH - Branch in RHOAI-Build-Config repo (e.g. rhoai-3.3)
#   ODH_BUILD_CONFIG_BRANCH   - Branch in ODH-Build-Config repo (default: main)
#   DEBUG                     - Set to "true" to keep intermediate files for inspection

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
cd "$REPO_ROOT"

CONFIG_FILE="component-params-env.yaml"
MANIFESTS_DIR="opt/manifests"
COMPONENTS_DIR="internal/controller/components"

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

# Validate paths from config don't contain traversal sequences
validate_path() {
    local path="$1"
    local label="$2"
    if [[ "$path" =~ \.\. ]] || [[ "$path" =~ ^/ ]]; then
        echo "ERROR: ${label} contains invalid path: ${path}" >&2
        exit 1
    fi
}

ODH_REPO="opendatahub-io/ODH-Build-Config"
RHOAI_REPO="red-hat-data-services/RHOAI-Build-Config"
ODH_BASE_URL="https://raw.githubusercontent.com/${ODH_REPO}/${ODH_BUILD_CONFIG_BRANCH}"
RHOAI_BASE_URL="https://raw.githubusercontent.com/${RHOAI_REPO}/${RHOAI_BUILD_CONFIG_BRANCH}"

# Colors (disabled if not a terminal or NO_COLOR is set)
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
    RED='\033[0;31m'
    YELLOW='\033[0;33m'
    GREEN='\033[0;32m'
    CYAN='\033[0;36m'
    BOLD='\033[1m'
    RESET='\033[0m'
else
    RED='' YELLOW='' GREEN='' CYAN='' BOLD='' RESET=''
fi

DEBUG="${DEBUG:-false}"
TMPDIR="${TMPDIR:-/tmp}"
WORKDIR=$(mktemp -d "${TMPDIR}/validate-related-images.XXXXXX")
if [ "$DEBUG" = "true" ]; then
    echo "DEBUG: Working directory: ${WORKDIR} (will NOT be deleted)"
else
    trap 'rm -rf "$WORKDIR"' EXIT
fi

# --- Pre-flight checks ---

if [ ! -f "$CONFIG_FILE" ]; then
    echo "ERROR: Config file not found: ${CONFIG_FILE}"
    exit 1
fi

if [ ! -d "$MANIFESTS_DIR" ]; then
    echo "ERROR: Manifests directory not found: ${MANIFESTS_DIR}"
    echo "Run 'make get-manifests' first."
    exit 1
fi

echo "Validating RELATED_IMAGE_* references per platform..."
echo "  ODH-Build-Config branch:  ${ODH_BUILD_CONFIG_BRANCH}"
echo "  RHOAI-Build-Config branch: ${RHOAI_BUILD_CONFIG_BRANCH}"
echo ""

# --- Step 1: Fetch build config RELATED_IMAGE names ---

fetch_and_extract() {
    local url="$1"
    local output="$2"
    local label="$3"

    local temp_file
    temp_file=$(mktemp "${WORKDIR}/fetched.XXXXXX.yaml")

    if ! curl -sfL --max-filesize 10485760 --connect-timeout 10 --max-time 30 \
            "$url" -o "$temp_file" 2>/dev/null; then
        echo "WARNING: Failed to fetch ${label}: ${url}"
        rm -f "$temp_file"
        return 1
    fi
    grep -oh 'RELATED_IMAGE_[A-Z0-9_]\+' "$temp_file" >> "$output" 2>/dev/null || true
    rm -f "$temp_file"
}

fetch_repo_images() {
    local label="$1"
    local base_url="$2"
    local output="$3"
    local fetch_failed=0

    touch "$output"
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

ODH_BUILD_CONFIG="$WORKDIR/odh-build-config.txt"
RHOAI_BUILD_CONFIG="$WORKDIR/rhoai-build-config.txt"

# --- Step 2: Load known issues from config ---
# Store as a file: each line is "RELATED_IMAGE_NAME|jira|reason"

KNOWN_ISSUES_FILE="$WORKDIR/known-issues.txt"
KNOWN_ISSUES_MATCHED="$WORKDIR/known-issues-matched.txt"
touch "$KNOWN_ISSUES_FILE" "$KNOWN_ISSUES_MATCHED"
yq -r '.known_issues[] | .image + "|" + .jira + "|" + .reason' "$CONFIG_FILE" 2>/dev/null \
    > "$KNOWN_ISSUES_FILE" || true

is_known_issue() {
    grep -q "^$1|" "$KNOWN_ISSUES_FILE"
}

get_known_issue_info() {
    grep "^$1|" "$KNOWN_ISSUES_FILE" | head -1 | cut -d'|' -f2,3 | tr '|' ' - '
}

record_known_issue_match() {
    local image="$1"
    local platform="$2"
    grep "^${image}|" "$KNOWN_ISSUES_FILE" | head -1 | while IFS='|' read -r img jira reason; do
        echo "${platform}|${img}|${jira}|${reason}" >> "$KNOWN_ISSUES_MATCHED"
    done
}

# --- Step 3: Extract map entries per component ---
# For each component dir, extract Go map entries: "key": "RELATED_IMAGE_*"
# Output: component/key/RELATED_IMAGE_VALUE lines

extract_image_param_map() {
    local comp_dir="$1"
    local comp_name
    comp_name=$(basename "$comp_dir")

    # Find all "key": "RELATED_IMAGE_*" patterns in Go source (excluding tests)
    # Output format: component/key/RELATED_IMAGE_VALUE/source_file:line
    grep -rn '"[^"]*"[[:space:]]*:[[:space:]]*"RELATED_IMAGE_[A-Z0-9_]*"' "$comp_dir" \
        --include='*.go' --exclude='*_test.go' 2>/dev/null | while IFS= read -r match; do
        local source key value content
        source=$(echo "$match" | cut -d: -f1,2)  # file:line
        content=$(echo "$match" | cut -d: -f3-)   # the matched content
        key=$(echo "$content" | sed 's/.*"\([^"]*\)"[[:space:]]*:.*/\1/')
        value=$(echo "$content" | sed 's/.*:[[:space:]]*//' | grep -o 'RELATED_IMAGE_[A-Z0-9_]\+')
        [ -z "$key" ] || [ -z "$value" ] && continue
        echo "${comp_name}/${key}/${value}/${source}"
    done
}

mkdir -p "$WORKDIR/components"
for comp_dir in "$COMPONENTS_DIR"/*/; do
    comp_name=$(basename "$comp_dir")
    [ "$comp_name" = "registry" ] && continue
    [ ! -d "$comp_dir" ] && continue

    extract_image_param_map "$comp_dir" | sort -u > "$WORKDIR/components/${comp_name}.txt" || true

    # Remove empty files (components without RELATED_IMAGE_* references)
    [ ! -s "$WORKDIR/components/${comp_name}.txt" ] && rm -f "$WORKDIR/components/${comp_name}.txt"
done

# --- Step 4: Discover all params.env files and collect keys ---
# Use the union of ALL params.env keys (not per-platform) because some components
# reuse the same overlay for both ODH and RHOAI (e.g. kserve uses overlays/odh/ for both).

ERRORS=0
WARNINGS=0

ALL_PARAMS_ENV_KEYS="$WORKDIR/all-params-env-keys.txt"
ALL_PARAMS_ENV_FILES="$WORKDIR/all-params-env-files.txt"
touch "$ALL_PARAMS_ENV_KEYS" "$ALL_PARAMS_ENV_FILES"

find "$MANIFESTS_DIR" -type f \( -name 'params.env' -o -name 'params-*.env' \) | sort > "$WORKDIR/params-env-list.txt"
while IFS= read -r pfile; do
    validate_path "$pfile" "params.env path"
    grep -o '^[^#=]*' "$pfile" | sed 's/[[:space:]]*$//' >> "$ALL_PARAMS_ENV_KEYS"
    echo "$pfile" >> "$ALL_PARAMS_ENV_FILES"
done < "$WORKDIR/params-env-list.txt"

sort -u -o "$ALL_PARAMS_ENV_KEYS" "$ALL_PARAMS_ENV_KEYS"

PARAMS_ENV_COUNT=$(wc -l < "$ALL_PARAMS_ENV_FILES" | tr -d ' ')
PARAMS_KEY_COUNT=$(wc -l < "$ALL_PARAMS_ENV_KEYS" | tr -d ' ')
echo ""
echo "Discovered ${PARAMS_ENV_COUNT} params.env files with ${PARAMS_KEY_COUNT} unique keys"

# --- Step 5: Fetch both build configs ---

echo ""
fetch_repo_images "RHOAI-Build-Config (${RHOAI_BUILD_CONFIG_BRANCH})" "$RHOAI_BASE_URL" "$RHOAI_BUILD_CONFIG"
echo ""
fetch_repo_images "ODH-Build-Config (${ODH_BUILD_CONFIG_BRANCH})" "$ODH_BASE_URL" "$ODH_BUILD_CONFIG"

ODH_LABEL="ODH (${ODH_BUILD_CONFIG_BRANCH})"
RHOAI_LABEL="RHOAI (${RHOAI_BUILD_CONFIG_BRANCH})"

# --- Step 6: Unified validation ---
# Build a single list of all RELATED_IMAGE entries to check, then validate each once.
# Format: RELATED_IMAGE|key|component|source|has_map_entry
# For unmapped refs (os.Getenv etc.), key is empty.

ALL_ENTRIES="$WORKDIR/all-entries.txt"
touch "$ALL_ENTRIES"

# Collect from map entries
for comp_file in "$WORKDIR/components/"*.txt; do
    [ ! -f "$comp_file" ] && continue
    comp_name=$(basename "$comp_file" .txt)
    while IFS='/' read -r _ key related_image source; do
        echo "${related_image}|${key}|${comp_name}|${source}|true" >> "$ALL_ENTRIES"
    done < "$comp_file"
done

# Collect unmapped refs (os.Getenv, function args, etc.)
cat "$WORKDIR/components/"*.txt 2>/dev/null | cut -d'/' -f3 | sort -u > "$WORKDIR/mapped-images.txt"
grep -roh 'RELATED_IMAGE_[A-Z0-9_]\+' internal/ \
    --include='*.go' --exclude='*_test.go' \
    | sort -u > "$WORKDIR/all-refs.txt"
grep -roh '"RELATED_IMAGE_[A-Z0-9_]*"[[:space:]]*:' internal/ \
    --include='*.go' --exclude='*_test.go' 2>/dev/null \
    | grep -oh 'RELATED_IMAGE_[A-Z0-9_]\+' \
    | sort -u > "$WORKDIR/map-keys.txt"
comm -23 "$WORKDIR/all-refs.txt" "$WORKDIR/map-keys.txt" > "$WORKDIR/all-env-refs.txt"
comm -23 "$WORKDIR/all-env-refs.txt" "$WORKDIR/mapped-images.txt" > "$WORKDIR/unmapped-refs.txt"
while IFS= read -r img; do
    src=$(grep -rn "$img" internal/ --include='*.go' --exclude='*_test.go' | head -1)
    src_file=""
    [ -n "$src" ] && src_file="${src%%:*}:$(echo "$src" | cut -d: -f2)"
    echo "${img}||unmapped|${src_file}|false" >> "$ALL_ENTRIES"
done < "$WORKDIR/unmapped-refs.txt"

# Deduplicate by RELATED_IMAGE and validate each once
ERRORS_FILE="$WORKDIR/errors.txt"
WARNINGS_FILE="$WORKDIR/warnings.txt"
touch "$ERRORS_FILE" "$WARNINGS_FILE"

# Get unique RELATED_IMAGE names
sort -u -t'|' -k1,1 "$ALL_ENTRIES" | cut -d'|' -f1 | sort -u > "$WORKDIR/unique-images.txt"

while IFS= read -r related_image; do
    in_odh=false
    in_rhoai=false
    grep -qx "$related_image" "$ODH_BUILD_CONFIG" && in_odh=true || true
    grep -qx "$related_image" "$RHOAI_BUILD_CONFIG" && in_rhoai=true || true

    # Collect all references to this RELATED_IMAGE
    entries=$(grep "^${related_image}|" "$ALL_ENTRIES")
    has_map_entry=$(echo "$entries" | grep '|true$' | head -1 || true)

    # Check params.env (only relevant for map entries)
    in_params_env=false
    params_info="Not in any params.env"
    if [ -n "$has_map_entry" ]; then
        first_key=$(echo "$has_map_entry" | cut -d'|' -f2)
        if grep -qx "$first_key" "$ALL_PARAMS_ENV_KEYS"; then
            in_params_env=true
            pfiles=$(grep -rl "^${first_key}=" "$MANIFESTS_DIR" --include='params.env' --include='params-*.env' 2>/dev/null | tr '\n' ',' | sed 's/,$//' | sed 's/,/, /g')
            params_info="In params.env: ${pfiles}"
        fi
    else
        params_info="Not in any map (used via os.Getenv or function arg)"
    fi

    # Skip if everything is OK
    $in_params_env && $in_odh && $in_rhoai && continue
    [ -z "$has_map_entry" ] && $in_odh && $in_rhoai && continue

    # Build config status
    bc_status=""
    if $in_odh && $in_rhoai; then
        bc_status="${GREEN}ODH ✓${RESET}, ${GREEN}RHOAI ✓${RESET}"
    elif $in_odh; then
        bc_status="${GREEN}ODH ✓${RESET}, ${RED}RHOAI ✗${RESET}"
    elif $in_rhoai; then
        bc_status="${RED}ODH ✗${RESET}, ${GREEN}RHOAI ✓${RESET}"
    else
        bc_status="${RED}ODH ✗${RESET}, ${RED}RHOAI ✗${RESET}"
    fi

    # Known issue check
    known_issue_info=""
    if is_known_issue "$related_image"; then
        known_issue_info=$(get_known_issue_info "$related_image")
    fi

    # Severity: ERROR if missing from any build config, WARNING if only missing from params.env
    is_error=false
    if ! $in_odh || ! $in_rhoai; then
        is_error=true
    fi
    if [ -z "$has_map_entry" ] && ! $in_odh && ! $in_rhoai; then
        is_error=true
    fi
    if ! $in_params_env && $in_odh && $in_rhoai; then
        is_error=false
    fi

    target_file="$ERRORS_FILE"
    if [ -n "$known_issue_info" ]; then
        target_file="$WARNINGS_FILE"
        record_known_issue_match "$related_image" "build-config"
    elif ! $is_error; then
        target_file="$WARNINGS_FILE"
    fi

    {
        if [ "$target_file" = "$ERRORS_FILE" ]; then
            printf "  ${RED}%s${RESET}\n" "$related_image"
        else
            printf "  ${YELLOW}%s${RESET}" "$related_image"
            [ -n "$known_issue_info" ] && printf " [known issue: %s]" "$known_issue_info"
            printf "\n"
        fi

        # List all references (component/key/source)
        echo "$entries" | while IFS='|' read -r _ ekey ecomp esource emap; do
            if [ "$emap" = "true" ]; then
                echo "    Component: ${ecomp}, key: ${ekey}, source: ${esource}"
            elif [ -n "$esource" ]; then
                echo "    Source: ${esource} (used via os.Getenv or function arg)"
            fi
        done

        echo "    ${params_info}"
        printf "    Build configs: ${bc_status}\n"
        echo ""
    } >> "$target_file"
done < "$WORKDIR/unique-images.txt"

# --- Print results: errors first, then warnings ---

ERRORS=0
WARNINGS=0

if [ -s "$ERRORS_FILE" ]; then
    ERRORS=$(grep -c '^  [^ ]' "$ERRORS_FILE" || true)
    echo ""
    printf "${RED}${BOLD}Errors:${RESET}\n"
    cat "$ERRORS_FILE"
fi

if [ -s "$WARNINGS_FILE" ]; then
    WARNINGS=$(grep -c '^  [^ ]' "$WARNINGS_FILE" || true)
    echo ""
    printf "${YELLOW}${BOLD}Warnings:${RESET}\n"
    cat "$WARNINGS_FILE"
fi

# --- Summary ---

echo ""
printf "${BOLD}=== Summary ===${RESET}\n"
echo ""

printf "  ${BOLD}Total:${RESET} "
if [ "$ERRORS" -gt 0 ]; then
    printf "${RED}%d error(s)${RESET}, " "$ERRORS"
else
    printf "${GREEN}%d error(s)${RESET}, " "$ERRORS"
fi
printf "${YELLOW}%d warning(s)${RESET}" "$WARNINGS"

if [ -s "$KNOWN_ISSUES_MATCHED" ]; then
    local_ki_count=$(sort -u "$KNOWN_ISSUES_MATCHED" | wc -l | tr -d ' ')
    printf ", ${CYAN}%d known issue(s)${RESET}" "$local_ki_count"
fi
echo ""

# Known issues detail
if [ -s "$KNOWN_ISSUES_MATCHED" ]; then
    echo ""
    printf "  ${CYAN}${BOLD}Known issues (downgraded to warnings):${RESET}\n"
    sort -u "$KNOWN_ISSUES_MATCHED" | while IFS='|' read -r _ ki_image ki_jira ki_reason; do
        printf "    ${CYAN}%s${RESET} - %s (%s)\n" "$ki_image" "$ki_reason" "$ki_jira"
    done
fi

if [ "$ERRORS" -gt 0 ]; then
    echo ""
    printf "${RED}Please ensure images are added to the build config repos and params.env before merging.${RESET}\n"
    echo "See: https://github.com/${ODH_REPO} and https://github.com/${RHOAI_REPO}"
    exit 1
fi

echo ""
printf "${GREEN}All RELATED_IMAGE_* references validated successfully.${RESET}\n"
