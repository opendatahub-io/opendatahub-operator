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
ADDITIONAL_IMAGES_PATH="bundle/additional-images-patch.yaml"
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

ADDITIONAL_IMAGES="${TMP_DIR}/build-config/${ADDITIONAL_IMAGES_PATH}"
if [[ -f "$ADDITIONAL_IMAGES" ]]; then
    while IFS='=' read -r name value; do
        [[ -n "$name" ]] && BUNDLE_IMAGES["$name"]="$value"
    done < <("${YQ}" eval '.additionalImages[] | .name + "=" + .value' "$ADDITIONAL_IMAGES" | tr -d '"')
fi

echo "Found ${#BUNDLE_IMAGES[@]} images total"

# Counters (use : to avoid set -e on zero arithmetic)
updated=0; skipped=0; unmapped=0
SKIPPED_LIST=()
UNMAPPED_LIST=()

# --- Helpers ---

update_params_env() {
    local file="${MANIFESTS_DIR}/$1" key="$2" value="$3"

    if [[ ! -f "$file" ]]; then
        return 1
    fi
    if ! grep -q "^${key}=" "$file"; then
        return 1
    fi

    sed -i "s#^${key}=.*#${key}=${value}#" "$file"
}

# --- Mapping tables ---
# Format: "relative/path/to/params.env:key" (space-separated for multiple targets)

declare -A IMAGE_MAP=(
    # Dashboard
    ["RELATED_IMAGE_ODH_DASHBOARD_IMAGE"]="dashboard/rhoai/onprem/params.env:odh-dashboard-image"

    # Workbench Controllers
    ["RELATED_IMAGE_ODH_KF_NOTEBOOK_CONTROLLER_IMAGE"]="workbenches/kf-notebook-controller/overlays/openshift/params.env:odh-kf-notebook-controller-image"
    ["RELATED_IMAGE_ODH_NOTEBOOK_CONTROLLER_IMAGE"]="workbenches/odh-notebook-controller/base/params.env:odh-notebook-controller-image"

    # Data Science Pipelines
    ["RELATED_IMAGE_ODH_DATA_SCIENCE_PIPELINES_OPERATOR_CONTROLLER_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_DSPO"
    ["RELATED_IMAGE_ODH_PIPELINES_COMPONENTS_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_PIPELINES_COMPONENTS"
    ["RELATED_IMAGE_ODH_MLMD_GRPC_SERVER_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_MLMDGRPC"
    ["RELATED_IMAGE_ODH_ML_PIPELINES_API_SERVER_V2_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_APISERVER"
    ["RELATED_IMAGE_ODH_ML_PIPELINES_PERSISTENCEAGENT_V2_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_PERSISTENCEAGENT"
    ["RELATED_IMAGE_ODH_ML_PIPELINES_SCHEDULEDWORKFLOW_V2_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_SCHEDULEDWORKFLOW"
    ["RELATED_IMAGE_ODH_ML_PIPELINES_DRIVER_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_DRIVER"
    ["RELATED_IMAGE_ODH_ML_PIPELINES_LAUNCHER_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_LAUNCHER"
    ["RELATED_IMAGE_ODH_DATA_SCIENCE_PIPELINES_ARGO_ARGOEXEC_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_ARGO_EXEC"
    ["RELATED_IMAGE_ODH_DATA_SCIENCE_PIPELINES_ARGO_WORKFLOWCONTROLLER_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_ARGO_WORKFLOWCONTROLLER"
    ["RELATED_IMAGE_DSP_MARIADB_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_MARIADB"
    ["RELATED_IMAGE_DSP_PROXYV2_IMAGE"]="datasciencepipelines/base/params.env:IMAGES_MLMDENVOY"

    # Model Controller
    ["RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE"]="modelcontroller/base/params.env:odh-model-controller"
    ["RELATED_IMAGE_ODH_MODEL_SERVING_API_IMAGE"]="modelcontroller/base/params.env:odh-model-serving-api"
    ["RELATED_IMAGE_ODH_OPENVINO_MODEL_SERVER_IMAGE"]="modelcontroller/base/params.env:ovms-image"
    ["RELATED_IMAGE_ODH_VLLM_CPU_IMAGE"]="modelcontroller/base/params.env:vllm-cpu-image"
    ["RELATED_IMAGE_RHAII_VLLM_GAUDI_IMAGE"]="modelcontroller/base/params.env:vllm-gaudi-image kserve/overlays/odh/params.env:kserve-llm-d-intel-gaudi"
    ["RELATED_IMAGE_RHAII_VLLM_CUDA_IMAGE"]="modelcontroller/base/params.env:vllm-cuda-image kserve/overlays/odh/params.env:kserve-llm-d-nvidia-cuda kserve/overlays/odh/params.env:kserve-llm-d"
    ["RELATED_IMAGE_RHAII_VLLM_ROCM_IMAGE"]="modelcontroller/base/params.env:vllm-rocm-image kserve/overlays/odh/params.env:kserve-llm-d-amd-rocm"
    ["RELATED_IMAGE_RHAII_VLLM_SPYRE_IMAGE"]="modelcontroller/base/params.env:vllm-spyre-image kserve/overlays/odh/params.env:kserve-llm-d-ibm-spyre"
    ["RELATED_IMAGE_RHAII_VLLM_CPU_IMAGE"]="modelcontroller/base/params.env:vllm-cpu-x86-image"
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
    ["RELATED_IMAGE_ODH_PYTHON_312_IMAGE"]="trustyai/overlays/rhoai/params.env:ragas-provider-image"

    # Ray
    ["RELATED_IMAGE_ODH_KUBERAY_OPERATOR_CONTROLLER_IMAGE"]="ray/openshift/params.env:odh-kuberay-operator-controller-image"

    # Model Registry
    ["RELATED_IMAGE_OSE_OAUTH_PROXY_IMAGE"]="modelregistry/overlays/odh/params.env:IMAGES_OAUTH_PROXY"
    ["RELATED_IMAGE_ODH_MODEL_REGISTRY_IMAGE"]="modelregistry/overlays/odh/params.env:IMAGES_REST_SERVICE"
    ["RELATED_IMAGE_ODH_MODEL_REGISTRY_OPERATOR_IMAGE"]="modelregistry/overlays/odh/params.env:IMAGES_MODELREGISTRY_OPERATOR"
    ["RELATED_IMAGE_ODH_MODEL_REGISTRY_JOB_ASYNC_UPLOAD_IMAGE"]="modelregistry/overlays/odh/params.env:IMAGES_JOBS_ASYNC_UPLOAD dashboard/rhoai/onprem/params.env:images-jobs-async-upload"
    ["RELATED_IMAGE_ODH_MODEL_METADATA_COLLECTION_IMAGE"]="modelregistry/overlays/odh/params.env:IMAGES_CATALOG_DATA"
    ["RELATED_IMAGE_ODH_MODEL_PERFORMANCE_DATA_IMAGE"]="modelregistry/overlays/odh/params.env:IMAGES_BENCHMARK_DATA"
    ["RELATED_IMAGE_POSTGRESQL_16_IMAGE"]="modelregistry/overlays/odh/params.env:IMAGES_POSTGRES"

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
    ["RELATED_IMAGE_ODH_MAAS_CONTROLLER_IMAGE"]="maas/overlays/odh/params.env:maas-controller-image"
    ["RELATED_IMAGE_ODH_AI_GATEWAY_PAYLOAD_PROCESSING_IMAGE"]="maas/overlays/odh/params.env:payload-processing-image"
    ["RELATED_IMAGE_UBI_MINIMAL_IMAGE"]="maas/overlays/odh/params.env:maas-api-key-cleanup-image"

    # MLflow
    ["RELATED_IMAGE_ODH_MLFLOW_IMAGE"]="mlflowoperator/base/params.env:MLFLOW_IMAGE"
    ["RELATED_IMAGE_ODH_MLFLOW_OPERATOR_IMAGE"]="mlflowoperator/base/params.env:MLFLOW_OPERATOR_IMAGE"

    # Spark
    ["RELATED_IMAGE_ODH_SPARK_OPERATOR_IMAGE"]="sparkoperator/overlays/rhoai/params.env:RELATED_IMAGE_ODH_SPARK_OPERATOR_IMAGE sparkoperator/overlays/rhoai/params.env:SPARK_OPERATOR_CONTROLLER_IMAGE sparkoperator/overlays/rhoai/params.env:SPARK_OPERATOR_WEBHOOK_IMAGE sparkoperator/default/params.env:RELATED_IMAGE_ODH_SPARK_OPERATOR_IMAGE sparkoperator/default/params.env:SPARK_OPERATOR_CONTROLLER_IMAGE sparkoperator/default/params.env:SPARK_OPERATOR_WEBHOOK_IMAGE"

    # Trainer
    ["RELATED_IMAGE_ODH_TRAINER_IMAGE"]="trainer/rhoai/params.env:odh-kubeflow-trainer-controller-image"

    # kube-rbac-proxy
    ["RELATED_IMAGE_OSE_KUBE_RBAC_PROXY_IMAGE"]="dashboard/rhoai/onprem/params.env:kube-rbac-proxy datasciencepipelines/base/params.env:kube-rbac-proxy workbenches/odh-notebook-controller/base/params.env:kube-rbac-proxy kserve/overlays/odh/params.env:kube-rbac-proxy trustyai/overlays/rhoai/params.env:kube-rbac-proxy modelregistry/overlays/odh/params.env:kube-rbac-proxy"

    # WVA
    ["RELATED_IMAGE_ODH_WORKLOAD_VARIANT_AUTOSCALER_CONTROLLER_IMAGE"]="wva/openshift/params.env:wva-controller-image"

    # Dashboard Modular Architecture
    ["RELATED_IMAGE_ODH_MOD_ARCH_MODEL_REGISTRY_IMAGE"]="dashboard/modular-architecture/params.env:model-registry-ui-image"
    ["RELATED_IMAGE_ODH_MOD_ARCH_GEN_AI_IMAGE"]="dashboard/modular-architecture/params.env:gen-ai-ui-image"
    ["RELATED_IMAGE_ODH_MOD_ARCH_MAAS_IMAGE"]="dashboard/modular-architecture/params.env:maas-ui-image"
    ["RELATED_IMAGE_ODH_MOD_ARCH_MLFLOW_IMAGE"]="dashboard/modular-architecture/params.env:mlflow-ui-image"
    ["RELATED_IMAGE_ODH_MOD_ARCH_EVAL_HUB_IMAGE"]="dashboard/modular-architecture/params.env:eval-hub-ui-image"
    ["RELATED_IMAGE_ODH_MOD_ARCH_AUTOML_IMAGE"]="dashboard/modular-architecture/params.env:automl-ui-image"
    ["RELATED_IMAGE_ODH_MOD_ARCH_AUTORAG_IMAGE"]="dashboard/modular-architecture/params.env:autorag-ui-image"
)

# Track which RELATED_IMAGE names are handled by explicit mappings
declare -A HANDLED_IMAGES
for name in "${!IMAGE_MAP[@]}"; do
    HANDLED_IMAGES["$name"]=1
done

for related_name in "${!IMAGE_MAP[@]}"; do
    new_value="${BUNDLE_IMAGES[$related_name]:-}"
    if [[ -z "$new_value" ]]; then
        SKIPPED_LIST+=("${related_name} (no bundle-patch entry)")
        : $(( skipped += 1 ))
        continue
    fi
    for target in ${IMAGE_MAP[$related_name]}; do
        file="${target%%:*}"
        key="${target#*:}"
        if update_params_env "$file" "$key" "$new_value"; then
            : $(( updated += 1 ))
        else
            SKIPPED_LIST+=("${related_name} -> ${file}:${key}")
            : $(( skipped += 1 ))
        fi
    done
done

NOTEBOOKS_PARAMS="${MANIFESTS_DIR}/workbenches/notebooks/rhoai/base/params.env"
TRAINER_PARAMS="${MANIFESTS_DIR}/trainer/rhoai/params.env"

for related_name in "${!BUNDLE_IMAGES[@]}"; do
    [[ -n "${HANDLED_IMAGES[$related_name]:-}" ]] && continue

    new_value="${BUNDLE_IMAGES[$related_name]}"
    search_pattern="${related_name#RELATED_IMAGE_}"
    search_pattern="${search_pattern%_IMAGE}"
    search_pattern=$(echo "$search_pattern" | tr '[:upper:]_' '[:lower:]-')

    matched=false

    if [[ "$related_name" == *_WORKBENCH_* || "$related_name" == *_PIPELINE_RUNTIME_* ]] && [[ -f "$NOTEBOOKS_PARAMS" ]]; then
        matching_key=$(grep -o "^${search_pattern}-[a-z0-9-]*" "$NOTEBOOKS_PARAMS" | sort -V | tail -1) || true
        if [[ -n "$matching_key" ]]; then
            sed -i "s#^${matching_key}=.*#${matching_key}=${new_value}#" "$NOTEBOOKS_PARAMS"
            : $(( updated += 1 ))
            matched=true
        fi
    fi

    if [[ "$related_name" == *_TRAINING_* || "$related_name" == *_TH06_* ]] && [[ -f "$TRAINER_PARAMS" ]]; then
        matching_key=$(grep -o "^${search_pattern}-image" "$TRAINER_PARAMS" || grep -o "^${search_pattern}" "$TRAINER_PARAMS" || true)
        if [[ -n "$matching_key" ]]; then
            sed -i "s#^${matching_key}=.*#${matching_key}=${new_value}#" "$TRAINER_PARAMS"
            : $(( updated += 1 ))
            matched=true
        fi
    fi

    if [[ "$matched" == "false" ]]; then
        UNMAPPED_LIST+=("${related_name}")
        : $(( unmapped += 1 ))
    fi
done

# --- Summary ---

echo ""
echo "=== Summary ==="
echo "Updated: ${updated}  Skipped: ${skipped}  Unmapped: ${unmapped}"

if [[ ${#SKIPPED_LIST[@]} -gt 0 ]]; then
    echo ""
    echo "Skipped:"
    for item in "${SKIPPED_LIST[@]}"; do
        echo "  - ${item}"
    done
fi

if [[ ${#UNMAPPED_LIST[@]} -gt 0 ]]; then
    echo ""
    echo "Unmapped:"
    IFS=$'\n' sorted=($(sort <<<"${UNMAPPED_LIST[*]}")); unset IFS
    for item in "${sorted[@]}"; do
        echo "  - ${item}"
    done
fi
