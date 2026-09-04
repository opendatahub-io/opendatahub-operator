package kserve

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	types "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	moduleName    = componentApi.KserveComponentName
	crName        = componentApi.KserveInstanceName
	finalizerName = "platform.opendatahub.io/finalizer"

	LLMInferenceServiceDependencies       = componentApi.KserveKind + "LLMInferenceServiceDependencies"
	LLMInferenceServiceWideEPDependencies = componentApi.KserveKind + "LLMInferenceServiceWideEPDependencies"
)

type handler struct {
	modules.BaseHandler
}

func NewHandler() *handler {
	return &handler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:            moduleName,
				CRName:          crName,
				ManifestDir:     "kserve-module-operator",
				DeploymentName:  "kserve-module-controller-manager",
				GVK:             gvk.Kserve,
				ControllerImage: "RELATED_IMAGE_ODH_KSERVE_MODULE_OPERATOR_IMAGE",
				SubmoduleConditions: []modules.SubmoduleCondition{
					{
						SourceConditionType: LLMInferenceServiceDependencies,
						DSCConditionType:    LLMInferenceServiceDependencies,
					},
					{
						SourceConditionType: LLMInferenceServiceWideEPDependencies,
						DSCConditionType:    LLMInferenceServiceWideEPDependencies,
					},
				},
				// Keep in sync with kserve-module/pkg/kservemodule/images.go
				// and ODH-Build-Config bundle-patch.yaml.
				RelatedImages: []string{
					"RELATED_IMAGE_ODH_GUARDRAILS_DETECTOR_HUGGINGFACE_RUNTIME_IMAGE",
					"RELATED_IMAGE_ODH_KSERVE_AUTOGLUON_SERVER_IMAGE",
					"RELATED_IMAGE_ODH_KSERVE_AGENT_IMAGE",
					"RELATED_IMAGE_ODH_KSERVE_CONTROLLER_IMAGE",
					"RELATED_IMAGE_ODH_KSERVE_LLMISVC_CONTROLLER_IMAGE",
					"RELATED_IMAGE_ODH_KSERVE_LOCALMODEL_CONTROLLER_IMAGE",
					"RELATED_IMAGE_ODH_KSERVE_LOCALMODELNODE_AGENT_IMAGE",
					"RELATED_IMAGE_ODH_KSERVE_ROUTER_IMAGE",
					"RELATED_IMAGE_ODH_KSERVE_STORAGE_INITIALIZER_IMAGE",
					"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
					"RELATED_IMAGE_ODH_LATENCY_PREDICTOR_PREDICTION_IMAGE",
					"RELATED_IMAGE_ODH_LATENCY_PREDICTOR_TRAINING_IMAGE",
					"RELATED_IMAGE_ODH_LLM_D_ROUTER_DISAGG_SIDECAR_IMAGE",
					"RELATED_IMAGE_ODH_LLM_D_ROUTER_ENDPOINT_PICKER_IMAGE",
					"RELATED_IMAGE_ODH_MLSERVER_CUDA_IMAGE",
					"RELATED_IMAGE_ODH_MLSERVER_IMAGE",
					"RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE",
					"RELATED_IMAGE_ODH_MODEL_SERVING_API_IMAGE",
					"RELATED_IMAGE_ODH_OPENVINO_MODEL_SERVER_IMAGE",
					"RELATED_IMAGE_ODH_VLLM_CPU_FAST_1_IMAGE",
					"RELATED_IMAGE_ODH_VLLM_CPU_FAST_1_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_ODH_VLLM_CPU_FAST_2_IMAGE",
					"RELATED_IMAGE_ODH_VLLM_CPU_FAST_2_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_ODH_VLLM_CPU_IMAGE",
					"RELATED_IMAGE_ODH_VLLM_CPU_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_ODH_WORKLOAD_VARIANT_AUTOSCALER_CONTROLLER_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_CPU_FAST_1_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_CPU_FAST_1_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_RHAII_VLLM_CPU_FAST_2_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_CPU_FAST_2_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_RHAII_VLLM_CPU_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_CPU_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_RHAII_VLLM_CPU_PZ_FAST_1_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_CPU_PZ_FAST_1_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_RHAII_VLLM_CPU_PZ_FAST_2_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_CPU_PZ_FAST_2_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_RHAII_VLLM_CPU_PZ_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_CPU_PZ_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_RHAII_VLLM_CUDA_FAST_1_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_CUDA_FAST_1_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_RHAII_VLLM_CUDA_FAST_2_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_CUDA_FAST_2_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_RHAII_VLLM_CUDA_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_CUDA_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_RHAII_VLLM_GAUDI_FAST_1_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_GAUDI_FAST_1_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_RHAII_VLLM_GAUDI_FAST_2_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_GAUDI_FAST_2_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_RHAII_VLLM_GAUDI_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_GAUDI_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_RHAII_VLLM_OMNI_CUDA_FAST_1_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_OMNI_CUDA_FAST_1_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_RHAII_VLLM_OMNI_CUDA_FAST_2_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_OMNI_CUDA_FAST_2_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_RHAII_VLLM_OMNI_CUDA_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_OMNI_CUDA_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_RHAII_VLLM_ROCM_FAST_1_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_ROCM_FAST_1_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_RHAII_VLLM_ROCM_FAST_2_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_ROCM_FAST_2_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_RHAII_VLLM_ROCM_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_ROCM_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_RHAII_VLLM_SPYRE_FAST_1_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_SPYRE_FAST_1_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_RHAII_VLLM_SPYRE_FAST_2_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_SPYRE_FAST_2_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_RHAII_VLLM_SPYRE_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_SPYRE_IMAGE_UPSTREAM_VERSION",
					"RELATED_IMAGE_ODH_UBI_MICRO_IMAGE",
				},
			},
		},
	}
}

func (h *handler) GetOperatorManifests(platform *modules.PlatformContext) modules.OperatorManifests {
	result := h.BaseHandler.GetOperatorManifests(platform)
	if platform != nil && platform.ManifestsBasePath != "" {
		result.Manifests = append(result.Manifests, types.ManifestInfo{
			Path:       platform.ManifestsBasePath,
			ContextDir: "connectionAPI",
		})
	}
	return result
}

func (h *handler) PopulatePlatformModule(pm *configv1alpha1.PlatformModules, dscCtx *modules.DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSC == nil {
		return
	}
	ms := dscCtx.DSC.Spec.Components.Kserve.ManagementState
	if ms == "" {
		ms = operatorv1.Removed
	}
	pm.Kserve.ManagementState = ms
}

func (h *handler) IsEnabled(modules *configv1alpha1.PlatformModules) bool {
	return modules != nil && modules.Kserve.ManagementState == operatorv1.Managed
}

func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	dscCtx *modules.DSCContext,
	_ *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	if dscCtx == nil || dscCtx.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build kserve CR")
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&dscCtx.DSC.Spec.Components.Kserve.KserveCommonSpec)
	if err != nil {
		return nil, fmt.Errorf("failed to convert KserveCommonSpec to unstructured: %w", err)
	}
	delete(spec, "modelsAsService")

	// Inject cross-component ModelRegistry state.
	// ModelRegistry is a separate DSC component, not a Kserve sub-component.
	// We forward its management state so kserve-module can propagate it
	// to odh-model-controller's params.env as "modelregistry-state".
	mrState := string(dscCtx.DSC.Spec.Components.ModelRegistry.ManagementState)
	if mrState == "" {
		mrState = string(operatorv1.Removed)
	}
	spec["modelRegistry"] = map[string]any{
		"managementState": mrState,
	}

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": spec,
		},
	}
	u.SetGroupVersionKind(h.Config.GVK)
	u.SetName(h.Config.CRName)

	return u, nil
}
