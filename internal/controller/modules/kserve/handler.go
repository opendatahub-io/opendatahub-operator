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
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	moduleName = componentApi.KserveComponentName
	crName     = componentApi.KserveInstanceName
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
				GVK:             gvk.Kserve,
				ControllerImage: "RELATED_IMAGE_ODH_KSERVE_MODULE_OPERATOR_IMAGE",
				// Keep in sync with kserve-module/pkg/kservemodule/images.go
				// and ODH-Build-Config bundle-patch.yaml.
				RelatedImages: []string{
					"RELATED_IMAGE_ODH_CAIKIT_NLP_IMAGE",
					"RELATED_IMAGE_ODH_GUARDRAILS_DETECTOR_HUGGINGFACE_RUNTIME_IMAGE",
					"RELATED_IMAGE_ODH_KSERVE_AGENT_IMAGE",
					"RELATED_IMAGE_ODH_KSERVE_CONTROLLER_IMAGE",
					"RELATED_IMAGE_ODH_KSERVE_LLMISVC_CONTROLLER_IMAGE",
					"RELATED_IMAGE_ODH_KSERVE_LOCALMODEL_CONTROLLER_IMAGE",
					"RELATED_IMAGE_ODH_KSERVE_LOCALMODELNODE_AGENT_IMAGE",
					"RELATED_IMAGE_ODH_KSERVE_ROUTER_IMAGE",
					"RELATED_IMAGE_ODH_KSERVE_STORAGE_INITIALIZER_IMAGE",
					"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
					"RELATED_IMAGE_ODH_LLM_D_ROUTER_ENDPOINT_PICKER_IMAGE",
					"RELATED_IMAGE_ODH_LLM_D_KV_CACHE_IMAGE",
					"RELATED_IMAGE_ODH_LLM_D_ROUTER_DISAGG_SIDECAR_IMAGE",
					"RELATED_IMAGE_ODH_MLSERVER_IMAGE",
					"RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE",
					"RELATED_IMAGE_ODH_MODEL_SERVING_API_IMAGE",
					"RELATED_IMAGE_ODH_OPENVINO_MODEL_SERVER_IMAGE",
					"RELATED_IMAGE_ODH_VLLM_CPU_IMAGE",
					"RELATED_IMAGE_ODH_WORKLOAD_VARIANT_AUTOSCALER_CONTROLLER_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_CPU_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_CUDA_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_GAUDI_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_ROCM_IMAGE",
					"RELATED_IMAGE_RHAII_VLLM_SPYRE_IMAGE",
				},
			},
		},
	}
}

func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil {
		return false
	}
	if platform.DSC != nil {
		return platform.DSC.Spec.Components.Kserve.ManagementState == operatorv1.Managed
	}
	if platform.Platform != nil {
		return platform.Platform.Spec.Modules.Kserve.ManagementState == operatorv1.Managed
	}
	return false
}

func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform == nil {
		return nil, errors.New("platform context is nil, cannot build kserve CR")
	}

	var spec map[string]any

	switch {
	case platform.DSC != nil:
		var err error
		spec, err = runtime.DefaultUnstructuredConverter.ToUnstructured(&platform.DSC.Spec.Components.Kserve.KserveCommonSpec)
		if err != nil {
			return nil, fmt.Errorf("failed to convert KserveCommonSpec to unstructured: %w", err)
		}
	case platform.Platform != nil:
		spec = map[string]any{
			"managementState": string(platform.Platform.Spec.Modules.Kserve.ManagementState),
		}
	default:
		return nil, errors.New("neither DSC nor Platform is available, cannot build kserve CR")
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
