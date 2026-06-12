#!/usr/bin/env bash
set -euo pipefail

GITHUB_URL="https://github.com"
DST_MANIFESTS_DIR="./opt/manifests"
DST_CHARTS_DIR="./opt/charts"

# {ODH,RHOAI}_COMPONENT_MANIFESTS are lists of components repositories info to fetch the manifests
# in the format of "repo-org:repo-name:ref-name:source-folder" and key is the target folder under manifests/
# ref-name can be a branch name, tag name, or a commit SHA (7-40 hex characters)
# ref-name supports:
# 1. "branch" - tracks latest commit on branch (e.g., main)
# 2. "tag" - immutable reference (e.g., v1.0.0)
# 3. "branch@commit-sha" - tracks branch but pinned to specific commit (e.g., main@a1b2c3d4)

# ODH Component Manifests
declare -A ODH_COMPONENT_MANIFESTS=(
    ["workbenches/kf-notebook-controller"]="opendatahub-io:kubeflow:main@62ebba193e4f95dc48facc8a621d741494cd19af:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="opendatahub-io:kubeflow:main@62ebba193e4f95dc48facc8a621d741494cd19af:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="opendatahub-io:notebooks:main@57f1c4e71cbbfc5455bb60e38fe2f4c3d47d53e3:manifests"
    ["kserve"]="opendatahub-io:kserve:release-v0.17@46a82838f4d57be8469e4f1532a8ad1e36fbc29d:config"
    ["ray"]="opendatahub-io:kuberay:dev@ad21f8c87bbc1a9efe9ff399194abbdafd65aa08:ray-operator/config"
    ["trustyai"]="opendatahub-io:trustyai-service-operator:incubation@9756af205138b56f9d879c9a592c8e8ec3b4969d:config"
    ["modelregistry"]="opendatahub-io:model-registry-operator:main@6e10631042b0c31b1c27303a45caef9891fead36:config"
    ["trainingoperator"]="opendatahub-io:training-operator:stable@fc6f0f150c5728fcca8601a654d0a09324a8c121:manifests"
    ["datasciencepipelines"]="opendatahub-io:data-science-pipelines-operator:main@19c827062e91b16a0b2f1641b9cc53f1bff48b54:config"
    ["modelcontroller"]="opendatahub-io:odh-model-controller:incubating@3ad9e0bd01f9c44c41222d46866ca4492b84b4d4:config"
    ["feastoperator"]="opendatahub-io:feast:stable@2a4bb8241189343337e16a508b6a4baf92cb17db:infra/feast-operator/config"
    ["ogx"]="opendatahub-io:ogx-k8s-operator:odh@0ba12ea60949e1b551cebe63c3dc8be2dd4c0bd1:config"
    ["trainer"]="opendatahub-io:trainer:stable@fc9f4315b4ba88cfa03fabf29a730532411bab4c:manifests"
    ["maas"]="opendatahub-io:models-as-a-service:stable@e01c094491b8806bd6cdd75ddab7cc0e24188d3b:deployment"
    ["mlflowoperator"]="opendatahub-io:mlflow-operator:main@a9b2aa5ab0b505af3b449316937bde176f6decc6:config"
    ["sparkoperator"]="opendatahub-io:spark-operator:main@c2bad1c04553283b617846156d6dc2ee335662ac:config"
    ["wva"]="opendatahub-io:workload-variant-autoscaler:main@1a33d8559d4d56fcd0d4b9b2cc2afdbf6d832aee:config"
    ["aigateway"]="opendatahub-io:ai-gateway-operator:main@f0383d5d517c23249b6d7b73c6cb7c754036c1c3:config"
)

# RHOAI Component Manifests
declare -A RHOAI_COMPONENT_MANIFESTS=(
    ["workbenches/kf-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-3.5-ea.2@b288fcbbc9e90cace234e0961eb7c53be090d20c:components/notebook-controller/config"
    ["workbenches/odh-notebook-controller"]="red-hat-data-services:kubeflow:rhoai-3.5-ea.2@b288fcbbc9e90cace234e0961eb7c53be090d20c:components/odh-notebook-controller/config"
    ["workbenches/notebooks"]="red-hat-data-services:notebooks:rhoai-3.5-ea.2@a15f5837562b139854482038bb55ee67c135700f:manifests"
    ["kserve"]="red-hat-data-services:kserve:rhoai-3.5-ea.2@ede08d6fa735d5155428b6dc3a1c318447055ddb:config"
    ["ray"]="red-hat-data-services:kuberay:rhoai-3.5-ea.2@ab0d2b69fe910cde4692c220df6d02864ee36df9:ray-operator/config"
    ["trustyai"]="red-hat-data-services:trustyai-service-operator:rhoai-3.5-ea.2@d0f8bb863a998f8a7840f9573a5e65abbb9a84c2:config"
    ["modelregistry"]="red-hat-data-services:model-registry-operator:rhoai-3.5-ea.2@7c75bcad69f2b767924f0fd9debf2ccf398e2a59:config"
    ["trainingoperator"]="red-hat-data-services:training-operator:rhoai-3.5-ea.2@1eb40de7caa052dfd95c8319cb5c969fff16c246:manifests"
    ["datasciencepipelines"]="red-hat-data-services:data-science-pipelines-operator:rhoai-3.5-ea.2@e8775f3028a6a8573a07f8dbd63f37bb59445996:config"
    ["modelcontroller"]="red-hat-data-services:odh-model-controller:rhoai-3.5-ea.2@abf8460a75171c47a11d329570cf6521c493dbf9:config"
    ["feastoperator"]="red-hat-data-services:feast:rhoai-3.5-ea.2@c6acffd77c392bdd4f99588a302071cfdee0f711:infra/feast-operator/config"
    ["ogx"]="red-hat-data-services:ogx-k8s-operator:rhoai-3.5-ea.2@3b1b27851bf989f2729d55842ffe015e0519b740:config"
    ["trainer"]="red-hat-data-services:trainer:rhoai-3.5-ea.2@99344b31facebf8050200b832df868a358863210:manifests"
    ["maas"]="red-hat-data-services:maas-billing:rhoai-3.5-ea.2@1312edee9de6730e315e710aad32d79eebdfe38d:deployment"
    ["mlflowoperator"]="red-hat-data-services:mlflow-operator:rhoai-3.5-ea.2@2116e3d405199597f386fae5c3335676837a0f82:config"
    ["sparkoperator"]="red-hat-data-services:spark-operator:rhoai-3.5-ea.2@32039fb50b31ff4098d1c1645319ecb73e14da79:config"
    ["wva"]="red-hat-data-services:workload-variant-autoscaler:rhoai-3.5-ea.2@93c9e78bd1671f577e5efe6ec9248b32d25f8124:config"
    ["aigateway"]="red-hat-data-services:ai-gateway-operator:rhoai-3.5-ea.2:config"
)

# {ODH,RHOAI}_{CCM,COMPONENT}_CHARTS are lists of chart repositories info to fetch helm charts
# in the same format as manifests: "repo-org:repo-name:ref-name:source-folder"
# key is the target folder under charts/
# CCM_CHARTS: charts deployed by the CloudManager controller (dependencies)
# COMPONENT_CHARTS: charts deployed by individual component controllers

# ODH CloudManager Charts
declare -A ODH_CCM_CHARTS=(
    ["cert-manager-operator"]="opendatahub-io:odh-gitops:main@5fe2714d0ecaf5fafaf414952caf521ea9ebdaaf:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="opendatahub-io:odh-gitops:main@5fe2714d0ecaf5fafaf414952caf521ea9ebdaaf:charts/dependencies/lws-operator"
    ["sail-operator"]="opendatahub-io:odh-gitops:main@5fe2714d0ecaf5fafaf414952caf521ea9ebdaaf:charts/dependencies/sail-operator"
    ["gateway-api"]="opendatahub-io:odh-gitops:main@5fe2714d0ecaf5fafaf414952caf521ea9ebdaaf:charts/dependencies/gateway-api"
)

# ODH Component Charts
declare -A ODH_COMPONENT_CHARTS=(
    # Pin to odh-dashboard#8241 merge on main (webhook cert-manager templates, agentOps).
    # RHOAIENG-70528 RBAC ClusterRole expansion is pending — bump again when that lands.
    ["dashboard-operator"]="opendatahub-io:odh-dashboard:main@98c129fdd18ffe254b15cf8a3bb98ccfa00e09aa:dashboard-operator/charts/dashboard"
)

# RHOAI CloudManager Charts
declare -A RHOAI_CCM_CHARTS=(
    ["cert-manager-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5-ea.2@e6ad2249370ed4beb88d7888d9a24f846b48761b:charts/dependencies/cert-manager-operator"
    ["lws-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5-ea.2@e6ad2249370ed4beb88d7888d9a24f846b48761b:charts/dependencies/lws-operator"
    ["sail-operator"]="red-hat-data-services:odh-gitops:rhoai-3.5-ea.2@e6ad2249370ed4beb88d7888d9a24f846b48761b:charts/dependencies/sail-operator"
    ["gateway-api"]="red-hat-data-services:odh-gitops:rhoai-3.5-ea.2@e6ad2249370ed4beb88d7888d9a24f846b48761b:charts/dependencies/gateway-api"
)

# RHOAI Component Charts
declare -A RHOAI_COMPONENT_CHARTS=(
    ["dashboard-operator"]="red-hat-data-services:odh-dashboard:rhoai-3.5-ea.2@b2fe40cb02c3580f870729b1f36cde9a44146586:dashboard-operator/charts/dashboard"
)

# merge_charts merges CCM and component charts into COMPONENT_CHARTS, failing on duplicate keys.
merge_charts() {
    local -n _ccm=$1
    local -n _comp=$2
    for k in "${!_ccm[@]}"; do
        if [[ -n "${_comp[$k]+x}" ]]; then
            echo "ERROR: duplicate chart key '$k' in CCM and component charts" >&2
            exit 1
        fi
        COMPONENT_CHARTS["$k"]="${_ccm[$k]}"
    done
    for k in "${!_comp[@]}"; do
        COMPONENT_CHARTS["$k"]="${_comp[$k]}"
    done
}

# Select the appropriate manifest based on platform type
if [ "${ODH_PLATFORM_TYPE:-OpenDataHub}" = "OpenDataHub" ]; then
    echo "Cloning manifests and charts for ODH"
    declare -A COMPONENT_MANIFESTS=()
    for key in "${!ODH_COMPONENT_MANIFESTS[@]}"; do
        COMPONENT_MANIFESTS["$key"]="${ODH_COMPONENT_MANIFESTS[$key]}"
    done
    declare -A COMPONENT_CHARTS=()
    merge_charts ODH_CCM_CHARTS ODH_COMPONENT_CHARTS
else
    echo "Cloning manifests and charts for RHOAI"
    declare -A COMPONENT_MANIFESTS=()
    for key in "${!RHOAI_COMPONENT_MANIFESTS[@]}"; do
        COMPONENT_MANIFESTS["$key"]="${RHOAI_COMPONENT_MANIFESTS[$key]}"
    done
    declare -A COMPONENT_CHARTS=()
    merge_charts RHOAI_CCM_CHARTS RHOAI_COMPONENT_CHARTS
fi

# PLATFORM_MANIFESTS is a list of manifests that are contained in the operator repository. Please also add them to the
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
                IFS=':' read -r _ _ _ override_source_path <<< "$value"
                if [[ "$override_source_path" = /* || "$override_source_path" == *".."* ]]; then
                    echo "ERROR: Invalid source-folder '$override_source_path' (absolute paths and '..' are not allowed)."
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

    mkdir -p "$dir"
    pushd "$dir" &>/dev/null
    git init -q

    # Check if ref is in tracking format: branch@sha
    if [[ $ref =~ ^([a-zA-Z0-9_./-]+)@([a-f0-9]{7,40})$ ]]; then
        local commit_sha="${BASH_REMATCH[2]}"

        # For tracking format, fetch the specific commit SHA
        git remote add origin "$repo"
        if ! git fetch --depth 1 -q origin "$commit_sha"; then
            echo "ERROR: Failed to fetch from repository $repo"
            popd &>/dev/null
            return 1
        fi
        if ! git reset -q --hard "$commit_sha" 2>/dev/null; then
            echo "ERROR: Commit SHA $commit_sha not found in repository $repo"
            popd &>/dev/null
            return 1
        fi
    else
        # Try plain commit SHA first, then tag, then branch
        if [[ $ref =~ ^[a-f0-9]{7,40}$ ]] && git fetch -q --depth 1 "$repo" "$ref" && git reset -q --hard FETCH_HEAD; then
            :
        elif try_fetch_ref "$repo" "tags" "$ref" || try_fetch_ref "$repo" "heads" "$ref"; then
            :
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

    if [[ "${USE_LOCAL:-}" == "true" ]] && [[ -e "../${repo_name}" ]]; then
        echo "copying from adjacent checkout ..."
        mkdir -p "${dst_dir}/${target_path}"
        cp -a -- "../${repo_name}/${source_path}/." "${dst_dir}/${target_path}/"
        return
    fi

    if ! git_fetch_ref "${repo_url}" "${repo_ref}" "${repo_dir}"; then
        echo "ERROR: Failed to fetch ref '${repo_ref}' from '${repo_url}' for component '${key}'"
        return 1
    fi

    mkdir -p "${dst_dir}/${target_path}"
    cp -a -- "${repo_dir}/${source_path}/." "${dst_dir}/${target_path}/"
}

download_manifest() {
    download_repo_content "$1" "$2" "${DST_MANIFESTS_DIR}"
}

download_chart() {
    download_repo_content "$1" "$2" "${DST_CHARTS_DIR}"
}

# Track background job PIDs
declare -a pids=()
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
if [ "$failed" -eq 1 ]; then
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
    if [ "$failed" -eq 1 ]; then
        echo "One or more chart downloads failed"
        exit 1
    fi
fi

for key in "${!PLATFORM_MANIFESTS[@]}"; do
    source_path="${PLATFORM_MANIFESTS[$key]}"
    target_path="${key}"

    if [[ -d "${source_path}" && ! -L "${DST_MANIFESTS_DIR}/${target_path}" ]]; then
        echo -e "\033[32mSymlinking local manifest \033[33m${key}\033[32m:\033[0m ${source_path}"
        ln -s "$(pwd)/${source_path}" "${DST_MANIFESTS_DIR}/${target_path}"
    fi
done
