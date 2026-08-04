#!/usr/bin/env bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GITHUB_URL="https://github.com"
DST_MANIFESTS_DIR="./opt/manifests"
DST_CHARTS_DIR="./opt/charts"
CONFIG_FILE="${SCRIPT_DIR}/manifests-config.yaml"

if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "ERROR: $CONFIG_FILE not found"
    exit 1
fi

# read_yaml_section populates a bash associative array from a YAML section.
# Parses the fixed 4-level YAML structure (section → key → platform → field)
# using awk. No external dependencies beyond awk.
# Output format: "org:repo:ref:sourcePath" per entry (legacy download format).
read_yaml_section() {
    local -n _target=$1
    local section=$2
    local platform=$3

    while IFS='|' read -r key value; do
        [[ -z "$key" ]] && continue
        _target["$key"]="$value"
    done < <(awk -v section="$section" -v platform="$platform" '
        # Skip comments and blank lines
        /^[[:space:]]*#/ || /^[[:space:]]*$/ { next }
        {
            # Measure indentation (number of leading spaces)
            indent = 0
            for (i = 1; i <= length($0); i++) {
                if (substr($0, i, 1) == " ") indent++
                else break
            }
            # Strip leading/trailing whitespace, remove trailing colon for key lines
            line = $0
            gsub(/^[[:space:]]+|[[:space:]]+$/, "", line)
        }

        # Level 0: top-level section (components, ccmCharts, etc.)
        indent == 0 {
            if (line == section":") in_section = 1
            else in_section = 0
            cur_key = ""
            cur_platform = ""
            next
        }

        !in_section { next }

        # Level 1 (2-space): entry key
        indent == 2 {
            sub(/:$/, "", line)
            cur_key = line
            cur_platform = ""
            repo = ""; ref = ""; source_path = ""
            next
        }

        # Level 2 (4-space): platform
        indent == 4 {
            sub(/:$/, "", line)
            if (line == platform) cur_platform = line
            else cur_platform = ""
            repo = ""; ref = ""; source_path = ""
            next
        }

        # Level 3 (6-space): field values
        indent == 6 && cur_platform != "" {
            # Extract "key: value"
            field_name = line
            field_val = line
            sub(/:.*/, "", field_name)
            sub(/^[^:]+:[[:space:]]*/, "", field_val)

            # Strip surrounding quotes from value
            gsub(/^["'"'"']|["'"'"']$/, "", field_val)

            if (field_name == "repo") repo = field_val
            else if (field_name == "ref") ref = field_val
            else if (field_name == "sourcePath") source_path = field_val

            # Emit when all 3 fields collected
            if (repo != "" && ref != "" && source_path != "") {
                # Convert org/name → org:name
                gsub(/\//, ":", repo)
                printf "%s|%s:%s:%s\n", cur_key, repo, ref, source_path
                count++
                repo = ""; ref = ""; source_path = ""
            }
            next
        }
        END { printf "COUNT|%d\n", count }
    ' "$CONFIG_FILE")

    local actual_count="${_target[COUNT]:-0}"
    unset '_target[COUNT]'

    if [[ "$actual_count" -gt 0 ]]; then
        echo "  Parsed $actual_count entries from $section/$platform"
    fi
}

# Select platform
if [ "${ODH_PLATFORM_TYPE:-OpenDataHub}" = "OpenDataHub" ]; then
    platform="odh"
    echo "Cloning manifests and charts for ODH"
else
    platform="rhoai"
    echo "Cloning manifests and charts for RHOAI"
fi

# Build arrays from YAML
declare -A COMPONENT_MANIFESTS=()
read_yaml_section COMPONENT_MANIFESTS "components" "$platform"

declare -A CCM_CHARTS=()
read_yaml_section CCM_CHARTS "ccmCharts" "$platform"

declare -A COMPONENT_CHARTS=()
read_yaml_section COMPONENT_CHARTS "componentCharts" "$platform"

# Merge CCM charts into COMPONENT_CHARTS, checking for duplicates
for k in "${!CCM_CHARTS[@]}"; do
    if [[ -n "${COMPONENT_CHARTS[$k]+x}" ]]; then
        echo "ERROR: duplicate chart key '$k' in CCM and component charts" >&2
        exit 1
    fi
    COMPONENT_CHARTS["$k"]="${CCM_CHARTS[$k]}"
done

# PLATFORM_MANIFESTS is a list of manifests that are contained in the operator repository. Please also add them to the
# Dockerfile COPY instructions. Declaring them here causes this script to create a symlink in the manifests folder, so
# they can be easily modified during development, but during a container build, they must be copied into the proper
# location instead, as this script DOES NOT manage platform manifest files for a container build.
declare -A PLATFORM_MANIFESTS=(
    ["osd-configs"]="config/osd-configs"
    ["hardwareprofiles"]="config/hardwareprofiles"
    ["connectionAPI"]="config/connectionAPI"
)

# Allow overwriting repo using flags component=repo
# Updated pattern to accept commit SHAs (7-40 hex chars) and branch@sha format in addition to branches/tags
pattern="^[a-zA-Z0-9_.-]+:[a-zA-Z0-9_.-]+:([a-zA-Z0-9_./-]+|[a-zA-Z0-9_./-]+@[a-f0-9]{7,40}):[a-zA-Z0-9_./-]+$"
if [ "$#" -ge 1 ]; then
    for arg in "$@"; do
        if [[ $arg == --* ]]; then
            arg="${arg:2}"  # Remove the '--' prefix
            IFS="=" read -r key value <<< "$arg"
            if [[ -n "${COMPONENT_MANIFESTS[$key]}" ]]; then
                if [[ ! $value =~ $pattern ]]; then
                    echo "ERROR: The value '$value' does not match the expected format 'repo-org:repo-name:ref-name:source-folder'."
                    continue
                fi
                COMPONENT_MANIFESTS["$key"]=$value
            else
                echo "ERROR: '$key' does not exist in COMPONENT_MANIFESTS, it will be skipped."
                echo "Available components are: [${!COMPONENT_MANIFESTS[@]}]"
                exit 1
            fi
        else
            echo "Warning: Argument '$arg' does not follow the '--key=value' format."
        fi
    done
fi

TMP_DIR=$(mktemp -d -t "odh-manifests.XXXXXXXXXX")
trap '{ rm -rf -- "$TMP_DIR"; }' EXIT

function try_fetch_ref()
{
    local repo=$1
    local ref_type=$2  # "tags" or "heads"
    local ref=$3

    local git_ref="refs/$ref_type/$ref"
    local ref_name=$([[ $ref_type == "tags" ]] && echo "tag" || echo "branch")

    if git ls-remote --exit-code "$repo" "$git_ref" &>/dev/null; then
        if git fetch -q --depth 1 "$repo" "$git_ref" && git reset -q --hard FETCH_HEAD; then
            return 0
        else
            echo "ERROR: Failed to fetch $ref_name $ref from $repo"
            return 1
        fi
    fi
    return 1
}

function git_fetch_ref()
{
    local repo=$1
    local ref=$2
    local dir=$3

    mkdir -p $dir
    pushd $dir &>/dev/null
    git init -q

    # Check if ref is in tracking format: branch@sha
    if [[ $ref =~ ^([a-zA-Z0-9_./-]+)@([a-f0-9]{7,40})$ ]]; then
        local commit_sha="${BASH_REMATCH[2]}"

        # For tracking format, fetch the specific commit SHA
        git remote add origin $repo
        if ! git fetch --depth 1 -q origin $commit_sha; then
            echo "ERROR: Failed to fetch from repository $repo"
            popd &>/dev/null
            return 1
        fi
        if ! git reset -q --hard $commit_sha 2>/dev/null; then
            echo "ERROR: Commit SHA $commit_sha not found in repository $repo"
            popd &>/dev/null
            return 1
        fi
    else
        # Original logic for branches, tags, and plain commit SHAs
        # Try to fetch as tag first, then as branch
        if try_fetch_ref "$repo" "tags" "$ref" || try_fetch_ref "$repo" "heads" "$ref"; then
            # Successfully fetched tag or branch
            :  # no-op, we're done
        else
            echo "ERROR: '$ref' is not a valid branch, tag, or commit SHA in repository $repo"
            echo "You can check available refs with:"
            echo "  git ls-remote --heads $repo  # for branches"
            echo "  git ls-remote --tags $repo   # for tags"
            popd &>/dev/null
            return 1
        fi
    fi

    popd &>/dev/null
}

download_repo_content() {
    local key=$1
    local repo_info=$2
    local dst_dir=$3
    echo -e "\033[32mCloning repo \033[33m${key}\033[32m:\033[0m ${repo_info}"
    IFS=':' read -r -a repo_info <<< "${repo_info}"

    repo_org="${repo_info[0]}"
    repo_name="${repo_info[1]}"
    repo_ref="${repo_info[2]}"
    source_path="${repo_info[3]}"
    target_path="${key}"

    repo_url="${GITHUB_URL}/${repo_org}/${repo_name}"
    repo_dir="${TMP_DIR}/${dst_dir}/${key}"

    if [[ "${USE_LOCAL}" == "true" ]] && [[ -e ../${repo_name} ]]; then
        echo "copying from adjacent checkout ..."
        mkdir -p "${dst_dir}/${target_path}"
        cp -rf "../${repo_name}/${source_path}"/* "${dst_dir}/${target_path}"
        return
    fi

    if ! git_fetch_ref ${repo_url} ${repo_ref} ${repo_dir}; then
        echo "ERROR: Failed to fetch ref '${repo_ref}' from '${repo_url}' for component '${key}'"
        return 1
    fi

    mkdir -p "${dst_dir}/${target_path}"
    cp -rf "${repo_dir}/${source_path}"/* "${dst_dir}/${target_path}"
}

download_manifest() {
    download_repo_content "$1" "$2" "${DST_MANIFESTS_DIR}"
}

download_chart() {
    download_repo_content "$1" "$2" "${DST_CHARTS_DIR}"
}

# Track background job PIDs +declare -a pids=()
# Use parallel processing
for key in "${!COMPONENT_MANIFESTS[@]}"; do
    download_manifest "$key" "${COMPONENT_MANIFESTS[$key]}" &
    pids+=($!)
done
# Wait and check exit codes
failed=0
for pid in "${pids[@]}"; do
    if ! wait "$pid"; then
        failed=1
    fi
done
if [ $failed -eq 1 ]; then
    echo "One or more downloads failed"
    exit 1
fi

# Download charts in parallel
if [ ${#COMPONENT_CHARTS[@]} -gt 0 ]; then
    declare -a chart_pids=()
    for key in "${!COMPONENT_CHARTS[@]}"; do
        download_chart "$key" "${COMPONENT_CHARTS[$key]}" &
        chart_pids+=($!)
    done
    for pid in "${chart_pids[@]}"; do
        if ! wait "$pid"; then
            failed=1
        fi
    done
    if [ $failed -eq 1 ]; then
        echo "One or more chart downloads failed"
        exit 1
    fi
fi

for key in "${!PLATFORM_MANIFESTS[@]}"; do
    source_path="${PLATFORM_MANIFESTS[$key]}"
    target_path="${key}"

    if [[ -d ${source_path} && ! -L ${DST_MANIFESTS_DIR}/${target_path} ]]; then
        echo -e "\033[32mSymlinking local manifest \033[33m${key}\033[32m:\033[0m ${source_path}"
        ln -s $(pwd)/${source_path} ${DST_MANIFESTS_DIR}/${target_path}
    fi
done
