#!/usr/bin/env bash
# Updates RHOAI component manifests with pinned images from
# RHDS RHOAI-Build-Config bundle-patch.yaml.
#
# Usage: ./hack/update-rhoai-images.sh [--branch <branch>]
#
# Examples:
#   make update-rhoai-images
#   ./hack/update-rhoai-images.sh --branch rhoai-3.5

set -euo pipefail

BUILD_CONFIG_REPO="https://github.com/red-hat-data-services/RHOAI-Build-Config"
BUNDLE_PATCH_PATH="bundle/bundle-patch.yaml"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MANIFESTS_DIR="${MANIFESTS_DIR:-${SCRIPT_DIR}/opt/manifests}"
YQ="${YQ:-$(command -v yq || true)}"
[[ -n "$YQ" ]] || { echo "ERROR: yq is required but not found in PATH"; exit 1; }

while [[ $# -gt 0 ]]; do
    case $1 in
        --branch)
            [[ $# -ge 2 ]] || { echo "ERROR: --branch requires a value"; exit 1; }
            RHOAI_BRANCH="$2"; shift 2;;
        *)              echo "ERROR: Unknown argument: $1"; exit 1;;
    esac
done

if [[ -z "${RHOAI_BRANCH:-}" ]]; then
    echo "ERROR: RHOAI_BRANCH is not set. Use --branch flag or set RHOAI_BRANCH env var."
    exit 1
fi

echo "RHOAI branch: ${RHOAI_BRANCH}"
echo "Manifests dir: ${MANIFESTS_DIR}"

TMP_DIR=$(mktemp -d -t "rhoai-images.XXXXXXXXXX")
trap 'rm -rf "$TMP_DIR"' EXIT

# Download bundle-patch.yaml (only this branch)
echo "Downloading bundle-patch.yaml..."
if ! git clone --depth 1 -b "${RHOAI_BRANCH}" -q "${BUILD_CONFIG_REPO}" "${TMP_DIR}/build-config" 2>/dev/null; then
    echo "ERROR: Failed to clone ${BUILD_CONFIG_REPO} branch ${RHOAI_BRANCH}"
    exit 1
fi

BUNDLE_PATCH="${TMP_DIR}/build-config/${BUNDLE_PATCH_PATH}"
if [[ ! -f "$BUNDLE_PATCH" ]]; then
    echo "ERROR: ${BUNDLE_PATCH_PATH} not found in repo"
    exit 1
fi

declare -A BUNDLE_IMAGES
while IFS='=' read -r name value; do
    [[ -n "$name" ]] && BUNDLE_IMAGES["$name"]="$value"
done < <("${YQ}" eval '.patch.relatedImages[] | .name + "=" + .value' "$BUNDLE_PATCH" | tr -d '"')

echo "Found ${#BUNDLE_IMAGES[@]} images in bundle-patch.yaml"

# Counters (use : to avoid set -e on zero arithmetic)
updated=0; skipped=0; unmapped=0

# --- Helpers ---

update_params_env() {
    local file="${MANIFESTS_DIR}/$1" key="$2" value="$3"

    if [[ ! -f "$file" ]]; then
        echo "  SKIP: $1 (not found)"
        return 1
    fi
    if ! grep -q "^${key}=" "$file"; then
        echo "  SKIP: ${key} not in $1"
        return 1
    fi

    sed -i "s#^${key}=.*#${key}=${value}#" "$file"
    echo "  OK: $1 -> ${key}"
}

update_kustomize_image() {
    local file="${MANIFESTS_DIR}/$1" image_name="$2" new_image="$3"

    if [[ ! -f "$file" ]]; then
        echo "  SKIP: $1 (not found)"
        return 1
    fi
    if ! grep -q "name: ${image_name}" "$file"; then
        echo "  SKIP: image '${image_name}' not in $1"
        return 1
    fi

    local new_name
    if [[ "$new_image" == *"@sha256:"* ]]; then
        new_name="${new_image%%@*}"
        local digest="${new_image#*@}"
        "${YQ}" eval -i "
            (.images[] | select(.name == \"${image_name}\")).newName = \"${new_name}\" |
            (.images[] | select(.name == \"${image_name}\")).digest = \"${digest}\" |
            del(.images[] | select(.name == \"${image_name}\").newTag)
        " "$file"
    else
        new_name="${new_image%:*}"
        local tag="${new_image##*:}"
        "${YQ}" eval -i "
            (.images[] | select(.name == \"${image_name}\")).newName = \"${new_name}\" |
            (.images[] | select(.name == \"${image_name}\")).newTag = \"${tag}\"
        " "$file"
    fi
    echo "  OK: $1 -> images[${image_name}]"
}

# --- Mapping tables ---
# Format: "relative/path/to/params.env:key" (space-separated for multiple targets)

declare -A IMAGE_MAP=(
    # Dashboard
    ["RELATED_IMAGE_ODH_DASHBOARD_IMAGE"]="dashboard/rhoai/onprem/params.env:odh-dashboard-image dashboard/rhoai/addon/params.env:odh-dashboard-image"

    # Workbench Controllers
    ["RELATED_IMAGE_ODH_KF_NOTEBOOK_CONTROLLER_IMAGE"]="workbenches/kf-notebook-controller/overlays/openshift/params.env:odh-kf-notebook-controller-image"
    ["RELATED_IMAGE_ODH_NOTEBOOK_CONTROLLER_IMAGE"]="workbenches/odh-notebook-controller/base/params.env:odh-notebook-controller-image"

    # Data Science Pipelines
    ["RELATED_IMAGE_ODH_DATA_SCIENCE_PIPELINES_OPERATOR_CONTROLLER_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_DSPO"
    ["RELATED_IMAGE_ODH_MLMD_GRPC_SERVER_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_MLMDGRPC"
    ["RELATED_IMAGE_ODH_ML_PIPELINES_API_SERVER_V2_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_APISERVER"
    ["RELATED_IMAGE_ODH_ML_PIPELINES_PERSISTENCEAGENT_V2_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_PERSISTENCEAGENT"
    ["RELATED_IMAGE_ODH_ML_PIPELINES_SCHEDULEDWORKFLOW_V2_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_SCHEDULEDWORKFLOW"
    ["RELATED_IMAGE_ODH_ML_PIPELINES_DRIVER_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_DRIVER"
    ["RELATED_IMAGE_ODH_ML_PIPELINES_LAUNCHER_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_LAUNCHER"
    ["RELATED_IMAGE_ODH_DATA_SCIENCE_PIPELINES_ARGO_ARGOEXEC_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_ARGO_EXEC"
    ["RELATED_IMAGE_ODH_DATA_SCIENCE_PIPELINES_ARGO_WORKFLOWCONTROLLER_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_ARGO_WORKFLOWCONTROLLER"

    # Model Controller
    ["RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE"]="modelcontroller/base/params.env:odh-model-controller"
    ["RELATED_IMAGE_ODH_OPENVINO_MODEL_SERVER_IMAGE"]="modelcontroller/base/params.env:ovms-image"
    ["RELATED_IMAGE_ODH_VLLM_CPU_IMAGE"]="modelcontroller/base/params.env:vllm-cpu-image"
    ["RELATED_IMAGE_ODH_VLLM_GAUDI_IMAGE"]="modelcontroller/base/params.env:vllm-gaudi-image"
    ["RELATED_IMAGE_ODH_MLSERVER_IMAGE"]="modelcontroller/base/params.env:mlserver-image"
    ["RELATED_IMAGE_ODH_GUARDRAILS_DETECTOR_HUGGINGFACE_RUNTIME_IMAGE"]="modelcontroller/base/params.env:guardrails-detector-huggingface-runtime-image"

    # TrustyAI
    ["RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_IMAGE"]="trustyai/overlays/rhoai/params.env:trustyaiServiceImage"
    ["RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_OPERATOR_IMAGE"]="trustyai/overlays/rhoai/params.env:trustyaiOperatorImage"
    ["RELATED_IMAGE_ODH_TA_LMES_DRIVER_IMAGE"]="trustyai/overlays/rhoai/params.env:lmes-driver-image"
    ["RELATED_IMAGE_ODH_TA_LMES_JOB_IMAGE"]="trustyai/overlays/rhoai/params.env:lmes-pod-image"
    ["RELATED_IMAGE_ODH_FMS_GUARDRAILS_ORCHESTRATOR_IMAGE"]="trustyai/overlays/rhoai/params.env:guardrails-orchestrator-image"
    ["RELATED_IMAGE_ODH_BUILT_IN_DETECTOR_IMAGE"]="trustyai/overlays/rhoai/params.env:guardrails-built-in-detector-image"
    ["RELATED_IMAGE_ODH_TRUSTYAI_VLLM_ORCHESTRATOR_GATEWAY_IMAGE"]="trustyai/overlays/rhoai/params.env:guardrails-sidecar-gateway-image"
    ["RELATED_IMAGE_ODH_TRUSTYAI_NEMO_GUARDRAILS_SERVER_IMAGE"]="trustyai/overlays/rhoai/params.env:nemo-guardrails-image"
    ["RELATED_IMAGE_ODH_TRUSTYAI_GARAK_LLS_PROVIDER_DSP_IMAGE"]="trustyai/overlays/rhoai/params.env:garak-provider-image"
    ["RELATED_IMAGE_ODH_EVAL_HUB_IMAGE"]="trustyai/overlays/rhoai/params.env:evalHubImage"

    # Ray
    ["RELATED_IMAGE_ODH_KUBERAY_OPERATOR_CONTROLLER_IMAGE"]="ray/openshift/params.env:odh-kuberay-operator-controller-image"

    # Model Registry
    ["RELATED_IMAGE_ODH_MODEL_REGISTRY_IMAGE"]="modelregistry/overlays/odh/params.env:IMAGES_REST_SERVICE"
    ["RELATED_IMAGE_ODH_MODEL_REGISTRY_OPERATOR_IMAGE"]="modelregistry/overlays/odh/params.env:IMAGES_MODELREGISTRY_OPERATOR"
    ["RELATED_IMAGE_ODH_MODEL_REGISTRY_JOB_ASYNC_UPLOAD_IMAGE"]="modelregistry/overlays/odh/params.env:IMAGES_JOBS_ASYNC_UPLOAD dashboard/rhoai/onprem/params.env:images-jobs-async-upload dashboard/rhoai/addon/params.env:images-jobs-async-upload"
    ["RELATED_IMAGE_ODH_MODEL_METADATA_COLLECTION_IMAGE"]="modelregistry/overlays/odh/params.env:IMAGES_CATALOG_DATA modelregistry/overlays/odh/params.env:IMAGES_BENCHMARK_DATA"

    # Training Operator
    ["RELATED_IMAGE_ODH_TRAINING_OPERATOR_IMAGE"]="trainingoperator/rhoai/params.env:odh-training-operator-controller-image"

    # Feast
    ["RELATED_IMAGE_ODH_FEAST_OPERATOR_IMAGE"]="feastoperator/overlays/rhoai/params.env:RELATED_IMAGE_FEAST_OPERATOR"
    ["RELATED_IMAGE_ODH_FEATURE_SERVER_IMAGE"]="feastoperator/overlays/rhoai/params.env:RELATED_IMAGE_FEATURE_SERVER"

    # LLaMA Stack
    ["RELATED_IMAGE_ODH_LLAMA_STACK_K8S_OPERATOR_IMAGE"]="llamastackoperator/overlays/rhoai/params.env:RELATED_IMAGE_ODH_LLAMASTACK_OPERATOR"
    ["RELATED_IMAGE_ODH_LLAMA_STACK_CORE_IMAGE"]="llamastackoperator/overlays/rhoai/params.env:RELATED_IMAGE_RH_DISTRIBUTION"

    # KServe
    ["RELATED_IMAGE_ODH_KSERVE_CONTROLLER_IMAGE"]="kserve/overlays/odh/params.env:kserve-controller"
    ["RELATED_IMAGE_ODH_KSERVE_AGENT_IMAGE"]="kserve/overlays/odh/params.env:kserve-agent"
    ["RELATED_IMAGE_ODH_KSERVE_ROUTER_IMAGE"]="kserve/overlays/odh/params.env:kserve-router"
    ["RELATED_IMAGE_ODH_KSERVE_STORAGE_INITIALIZER_IMAGE"]="kserve/overlays/odh/params.env:kserve-storage-initializer"
    ["RELATED_IMAGE_ODH_LLM_D_INFERENCE_SCHEDULER_IMAGE"]="kserve/overlays/odh/params.env:kserve-llm-d-inference-scheduler"
    ["RELATED_IMAGE_ODH_LLM_D_ROUTING_SIDECAR_IMAGE"]="kserve/overlays/odh/params.env:kserve-llm-d-routing-sidecar"
    ["RELATED_IMAGE_ODH_LLM_D_KV_CACHE_IMAGE"]="kserve/overlays/odh/params.env:kserve-llm-d-uds-tokenizer"
    ["RELATED_IMAGE_ODH_KSERVE_LLMISVC_CONTROLLER_IMAGE"]="kserve/overlays/odh/params.env:llmisvc-controller"

    # MAAS
    ["RELATED_IMAGE_ODH_MAAS_API_IMAGE"]="maas/overlays/odh/params.env:maas-api-image"

    # MLflow
    ["RELATED_IMAGE_ODH_MLFLOW_IMAGE"]="mlflowoperator/base/params.env:MLFLOW_IMAGE"
    ["RELATED_IMAGE_ODH_MLFLOW_OPERATOR_IMAGE"]="mlflowoperator/base/params.env:MLFLOW_OPERATOR_IMAGE"

    # Spark
    ["RELATED_IMAGE_ODH_SPARK_OPERATOR_IMAGE"]="sparkoperator/overlays/rhoai/params.env:RELATED_IMAGE_SPARK_OPERATOR_IMAGE sparkoperator/overlays/rhoai/params.env:SPARK_OPERATOR_CONTROLLER_IMAGE sparkoperator/overlays/rhoai/params.env:SPARK_OPERATOR_WEBHOOK_IMAGE sparkoperator/default/params.env:RELATED_IMAGE_SPARK_OPERATOR_IMAGE sparkoperator/default/params.env:SPARK_OPERATOR_CONTROLLER_IMAGE sparkoperator/default/params.env:SPARK_OPERATOR_WEBHOOK_IMAGE"

    # Trainer
    ["RELATED_IMAGE_ODH_TRAINER_IMAGE"]="trainer/rhoai/params.env:odh-kubeflow-trainer-controller-image"

    # kube-rbac-proxy / kube-auth-proxy
    ["RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE"]="dashboard/rhoai/onprem/params.env:kube-rbac-proxy dashboard/rhoai/addon/params.env:kube-rbac-proxy workbenches/odh-notebook-controller/base/params.env:kube-rbac-proxy kserve/overlays/odh/params.env:kube-rbac-proxy"
    ["RELATED_IMAGE_OSE_KUBE_RBAC_PROXY_IMAGE"]="datasciencepipelines/base/params.env:kube-rbac-proxy"

    # Dashboard Modular Architecture
    ["RELATED_IMAGE_ODH_MOD_ARCH_MODEL_REGISTRY_IMAGE"]="dashboard/modular-architecture/params.env:model-registry-ui-image"
    ["RELATED_IMAGE_ODH_MOD_ARCH_GEN_AI_IMAGE"]="dashboard/modular-architecture/params.env:gen-ai-ui-image"
    ["RELATED_IMAGE_ODH_MOD_ARCH_MAAS_IMAGE"]="dashboard/modular-architecture/params.env:maas-ui-image"
    ["RELATED_IMAGE_ODH_MOD_ARCH_MLFLOW_IMAGE"]="dashboard/modular-architecture/params.env:mlflow-ui-image"
    ["RELATED_IMAGE_ODH_MOD_ARCH_EVAL_HUB_IMAGE"]="dashboard/modular-architecture/params.env:eval-hub-ui-image"
)

# Kustomize images: "kustomization.yaml_path:image_name"
## TODO: we should follow the same pattern and change that in WVA to  use params.env instead of hardcode in kustomization.yaml
declare -A KUSTOMIZE_IMAGE_MAP=(
    ["RELATED_IMAGE_ODH_WORKLOAD_VARIANT_AUTOSCALER_CONTROLLER_IMAGE"]="wva/manager/kustomization.yaml:controller"
)

# Track which RELATED_IMAGE names are handled by explicit mappings
declare -A HANDLED_IMAGES
for name in "${!IMAGE_MAP[@]}" "${!KUSTOMIZE_IMAGE_MAP[@]}"; do
    HANDLED_IMAGES["$name"]=1
done

echo "=== Updating params.env files ==="
for related_name in "${!IMAGE_MAP[@]}"; do
    new_value="${BUNDLE_IMAGES[$related_name]:-}"
    if [[ -z "$new_value" ]]; then
        echo "  WARN: No bundle-patch entry for ${related_name}"
        : $(( skipped += 1 ))
        continue
    fi
    # Word-splitting is intentional here: targets are space-separated "file:key" pairs
    for target in ${IMAGE_MAP[$related_name]}; do
        file="${target%%:*}"
        key="${target#*:}"
        if update_params_env "$file" "$key" "$new_value"; then
            : $(( updated += 1 ))
        else
            : $(( skipped += 1 ))
        fi
    done
done

echo "=== Updating kustomization.yaml image overrides ==="
for related_name in "${!KUSTOMIZE_IMAGE_MAP[@]}"; do
    new_value="${BUNDLE_IMAGES[$related_name]:-}"
    if [[ -z "$new_value" ]]; then
        echo "  WARN: No bundle-patch entry for ${related_name}"
        : $(( skipped += 1 ))
        continue
    fi
    for target in ${KUSTOMIZE_IMAGE_MAP[$related_name]}; do
        file="${target%%:*}"
        image_name="${target#*:}"
        if update_kustomize_image "$file" "$image_name" "$new_value"; then
            : $(( updated += 1 ))
        else
            : $(( skipped += 1 ))
        fi
    done
done

echo "=== Updating workbench/training images (pattern matching) ==="

NOTEBOOKS_PARAMS="${MANIFESTS_DIR}/workbenches/notebooks/rhoai/base/params.env"
TRAINER_PARAMS="${MANIFESTS_DIR}/trainer/rhoai/params.env"

for related_name in "${!BUNDLE_IMAGES[@]}"; do
    # Skip images handled by explicit mappings
    [[ -n "${HANDLED_IMAGES[$related_name]:-}" ]] && continue

    new_value="${BUNDLE_IMAGES[$related_name]}"

    # Convert: RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_MINIMAL_CPU_PY312_IMAGE
    #       -> odh-workbench-jupyter-minimal-cpu-py312
    search_pattern="${related_name#RELATED_IMAGE_}"  # strip prefix
    search_pattern="${search_pattern%_IMAGE}"         # strip suffix
    search_pattern=$(echo "$search_pattern" | tr '[:upper:]_' '[:lower:]-')

    matched=false

    # Workbench
    if [[ "$related_name" == *_WORKBENCH_* || "$related_name" == *_PIPELINE_RUNTIME_* ]] && [[ -f "$NOTEBOOKS_PARAMS" ]]; then
        # Match latest versioned key, e.g. odh-workbench-...-ubi9-2025-2
        matching_key=$(grep -o "^${search_pattern}-[a-z0-9-]*" "$NOTEBOOKS_PARAMS" | sort -V | tail -1) || true
        if [[ -n "$matching_key" ]]; then
            sed -i "s#^${matching_key}=.*#${matching_key}=${new_value}#" "$NOTEBOOKS_PARAMS"
            echo "  OK: workbenches/notebooks/rhoai/base/params.env -> ${matching_key}"
            : $(( updated += 1 ))
            matched=true
        fi
    fi

    # Training
    if [[ "$related_name" == *_TRAINING_* || "$related_name" == *_TH06_* ]] && [[ -f "$TRAINER_PARAMS" ]]; then
        # Try key with -image suffix first, then without
        matching_key=$(grep -o "^${search_pattern}-image" "$TRAINER_PARAMS" || grep -o "^${search_pattern}" "$TRAINER_PARAMS" || true)
        if [[ -n "$matching_key" ]]; then
            sed -i "s#^${matching_key}=.*#${matching_key}=${new_value}#" "$TRAINER_PARAMS"
            echo "  OK: trainer/rhoai/params.env -> ${matching_key}"
            : $(( updated += 1 ))
            matched=true
        fi
    fi

    if [[ "$matched" == "false" ]]; then
        echo "  UNMAPPED: ${related_name}"
        : $(( unmapped += 1 ))
    fi
done

# --- Summary ---

echo ""
echo "=== Summary ==="
echo "Updated: ${updated}  Skipped: ${skipped}  Unmapped: ${unmapped}"

if [[ $unmapped -gt 0 ]]; then
    echo "Unmapped images may be operator-level (MUST_GATHER, CLI, OPERATOR) or need mapping additions."
fi
