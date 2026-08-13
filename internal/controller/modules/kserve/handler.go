package kserve

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
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
		delete(spec, "modelsAsService")

		// Inject cross-component ModelRegistry state.
		// ModelRegistry is a separate DSC component, not a Kserve sub-component.
		// We forward its management state so kserve-module can propagate it
		// to odh-model-controller's params.env as "modelregistry-state".
		mrState := string(platform.DSC.Spec.Components.ModelRegistry.ManagementState)
		if mrState == "" {
			mrState = string(operatorv1.Removed)
		}
		spec["modelRegistry"] = map[string]any{
			"managementState": mrState,
		}
	case platform.Platform != nil:
		return nil, nil
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

// llmISvcConfigFinalizer is the finalizer set by the kserve module operator on
// LLMInferenceServiceConfig resources. The module operator controller is
// responsible for removing it, but during platform uninstall the operator
// Deployment is torn down before these resources are cleaned up, leaving the
// namespace stuck in Terminating.
const llmISvcConfigFinalizer = "serving.kserve.io/llmisvcconfig-finalizer"

// DeleteOperatorResources strips finalizers from LLMInferenceServiceConfig
// resources before deleting the kserve module operator resources. The kserve
// module operator sets llmisvcconfig-finalizer on these resources, but its
// controller is torn down as part of cleanup — if finalizers are not removed
// first, the namespace gets stuck in Terminating state.
func (h *handler) DeleteOperatorResources(ctx context.Context, cli client.Client, platform *modules.PlatformContext) error {
	ns := ""
	if platform != nil {
		ns = platform.ApplicationsNamespace
	}

	if err := StripLLMISvcConfigFinalizers(ctx, cli, ns); err != nil {
		return fmt.Errorf("stripping LLMInferenceServiceConfig finalizers: %w", err)
	}

	return h.BaseHandler.DeleteOperatorResources(ctx, cli, platform)
}

// StripLLMISvcConfigFinalizers lists all LLMInferenceServiceConfig resources
// in the given namespace and removes the llmisvcconfig-finalizer from each one.
// If namespace is empty, it searches across all namespaces. This is called both
// during module cleanup (DeleteOperatorResources) and during full operator
// uninstall (OperatorUninstall) to ensure finalizers are stripped regardless of
// module controller lifecycle timing.
func StripLLMISvcConfigFinalizers(ctx context.Context, cli client.Client, namespace string) error {
	log := logf.FromContext(ctx)

	configGVKs := []schema.GroupVersionKind{
		gvk.LLMInferenceServiceConfigV1Alpha1,
		gvk.LLMInferenceServiceConfigV1Alpha2,
	}

	for _, configGVK := range configGVKs {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(configGVK.GroupVersion().WithKind(configGVK.Kind + "List"))

		var listOpts []client.ListOption
		if namespace != "" {
			listOpts = append(listOpts, client.InNamespace(namespace))
		}

		if err := cli.List(ctx, list, listOpts...); err != nil {
			if meta.IsNoMatchError(err) {
				log.V(1).Info("LLMInferenceServiceConfig CRD not installed, skipping finalizer cleanup",
					"version", configGVK.Version)
				continue
			}
			return fmt.Errorf("listing LLMInferenceServiceConfig %s: %w", configGVK.Version, err)
		}

		for i := range list.Items {
			obj := &list.Items[i]
			if !controllerutil.ContainsFinalizer(obj, llmISvcConfigFinalizer) {
				continue
			}

			controllerutil.RemoveFinalizer(obj, llmISvcConfigFinalizer)
			if err := cli.Update(ctx, obj); err != nil {
				if k8serr.IsNotFound(err) {
					// Object was deleted between List and Update — finalizer
					// removal is moot, continue with remaining items.
					continue
				}
				return fmt.Errorf("removing finalizer from LLMInferenceServiceConfig %s/%s: %w",
					obj.GetNamespace(), obj.GetName(), err)
			}
			log.Info("stripped finalizer from LLMInferenceServiceConfig",
				"namespace", obj.GetNamespace(),
				"name", obj.GetName(),
				"version", configGVK.Version)
		}
	}

	return nil
}
