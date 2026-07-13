package workbenches

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	moduleName = componentApi.WorkbenchesComponentName
	crName     = componentApi.WorkbenchesInstanceName

	// ControllerDeploymentName is the rendered Helm release / Deployment name.
	ControllerDeploymentName = "workbenches-operator"

	controllerImage = "RELATED_IMAGE_ODH_WORKBENCHES_OPERATOR_IMAGE"
)

var relatedImages = []string{
	"RELATED_IMAGE_ODH_NOTEBOOK_CONTROLLER_IMAGE",
	"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
	"RELATED_IMAGE_ODH_KF_NOTEBOOK_CONTROLLER_IMAGE",
	"RELATED_IMAGE_ODH_WORKBENCH_CODESERVER_DATASCIENCE_CPU_PY312_IMAGE",
	"RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_DATASCIENCE_CPU_PY312_IMAGE",
	"RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_MINIMAL_CPU_PY312_IMAGE",
	"RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_MINIMAL_CUDA_PY312_IMAGE",
	"RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_MINIMAL_ROCM_PY312_IMAGE",
	"RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_PYTORCH_CUDA_PY312_IMAGE",
	"RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_PYTORCH_ROCM_PY312_IMAGE",
	"RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_TENSORFLOW_CUDA_PY312_IMAGE",
	"RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_TENSORFLOW_ROCM_PY312_IMAGE",
	"RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_TRUSTYAI_CPU_PY312_IMAGE",
	"RELATED_IMAGE_ODH_WORKBENCH_JUPYTER_PYTORCH_LLMCOMPRESSOR_CUDA_PY312_IMAGE",
	"RELATED_IMAGE_ODH_PIPELINE_RUNTIME_DATASCIENCE_CPU_PY312_IMAGE",
	"RELATED_IMAGE_ODH_PIPELINE_RUNTIME_MINIMAL_CPU_PY312_IMAGE",
	"RELATED_IMAGE_ODH_PIPELINE_RUNTIME_TENSORFLOW_CUDA_PY312_IMAGE",
	"RELATED_IMAGE_ODH_PIPELINE_RUNTIME_TENSORFLOW_ROCM_PY312_IMAGE",
	"RELATED_IMAGE_ODH_PIPELINE_RUNTIME_PYTORCH_CUDA_PY312_IMAGE",
	"RELATED_IMAGE_ODH_PIPELINE_RUNTIME_PYTORCH_ROCM_PY312_IMAGE",
	"RELATED_IMAGE_ODH_PIPELINE_RUNTIME_PYTORCH_LLMCOMPRESSOR_CUDA_PY312_IMAGE",
}

type handler struct {
	modules.BaseHandler
}

func NewHandler() *handler {
	return &handler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:              moduleName,
				CRName:            crName,
				ReleaseName:       ControllerDeploymentName,
				ChartDir:          moduleName,
				NamespaceValueKey: "operatorNamespace",
				DeploymentName:    ControllerDeploymentName,
				GVK:               gvk.Workbenches,
				ControllerImage:   controllerImage,
				RelatedImages:     relatedImages,
				Values: map[string]any{
					"relatedImages": emptyRelatedImageValues(),
					"webhooks": map[string]any{
						"port": 9443,
					},
				},
			},
		},
	}
}

// IsEnabled checks whether the Workbenches module should be deployed.
// In DSC mode, reads DSC.Spec.Components.Workbenches.ManagementState.
// In Platform mode (xKS), reads Platform.Spec.Modules.Workbenches.ManagementState.
func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil {
		return false
	}
	if platform.DSC != nil {
		return platform.DSC.Spec.Components.Workbenches.ManagementState == operatorv1.Managed
	}
	if platform.Platform != nil {
		return platform.Platform.Spec.Modules.Workbenches.ManagementState == operatorv1.Managed
	}
	return false
}

// BuildModuleCR projects platform configuration onto the Workbenches module CR.
// DSC-level managementState is projected into the module CR spec; orchestrator-only
// fields (gatewayDomain, platform, mlflowEnabled) are derived from PlatformContext.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform == nil {
		return nil, errors.New("platform context is nil, cannot build Workbenches CR")
	}

	var spec map[string]any

	switch {
	case platform.DSC != nil:
		var err error
		spec, err = runtime.DefaultUnstructuredConverter.ToUnstructured(
			&platform.DSC.Spec.Components.Workbenches.WorkbenchesCommonSpec,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to convert WorkbenchesCommonSpec to unstructured: %w", err)
		}
		spec["managementState"] = string(platform.DSC.Spec.Components.Workbenches.ManagementState)
		spec["mlflowEnabled"] = platform.DSC.Spec.Components.MLflowOperator.ManagementState == operatorv1.Managed
	case platform.Platform != nil:
		spec = map[string]any{
			"managementState": string(platform.Platform.Spec.Modules.Workbenches.ManagementState),
		}
	default:
		return nil, errors.New("neither DSC nor Platform is available, cannot build Workbenches CR")
	}

	spec["gatewayDomain"] = platform.GatewayDomain
	spec["platform"] = workbenchesPlatformType(platform.Release.Name)

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": spec,
		},
	}
	u.SetGroupVersionKind(h.Config.GVK)
	u.SetName(h.Config.CRName)

	return u, nil
}

// workbenchesPlatformType maps the platform release name to the module CR platform enum.
func workbenchesPlatformType(release common.Platform) string {
	switch release {
	case cluster.SelfManagedRhoai, cluster.ManagedRhoai:
		return "SelfManagedRhoai"
	default:
		return "OpenDataHub"
	}
}
