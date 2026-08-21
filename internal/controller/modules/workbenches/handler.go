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
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
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
	"RELATED_IMAGE_ODH_WORKBENCHES_CONTROLLER_IMAGE",
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
				SubmoduleConditions: []modules.SubmoduleCondition{
					{
						SourceConditionType: "WorkbenchesV2Ready",
						DSCConditionType:    "WorkbenchesV2Ready",
						StatusFieldName:     "WorkbenchesV2",
						IsEnabled: func(dscCtx *modules.DSCContext) bool {
							if dscCtx == nil || dscCtx.DSC == nil {
								return false
							}
							return dscCtx.DSC.Spec.Components.Workbenches.WorkbenchesV2.ManagementState == operatorv1.Managed
						},
					},
				},
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

func (h *handler) PopulatePlatformModule(pm *configv1alpha1.PlatformModules, dscCtx *modules.DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSC == nil {
		return
	}
	ms := dscCtx.DSC.Spec.Components.Workbenches.ManagementState
	if ms == "" {
		ms = operatorv1.Removed
	}
	pm.Workbenches.ManagementState = ms
}

// IsEnabled checks whether the Workbenches module should be deployed based on
// PlatformModules.Workbenches.ManagementState.
func (h *handler) IsEnabled(modules *configv1alpha1.PlatformModules) bool {
	return modules != nil && modules.Workbenches.ManagementState == operatorv1.Managed
}

// BuildModuleCR projects platform configuration onto the Workbenches module CR.
// DSC-level managementState and legacy workbenchNamespace are projected into the
// module CR spec for API parity; notebook-controller operands deploy into
// APPLICATIONS_NAMESPACE (injected separately). Orchestrator-only fields
// (gatewayDomain, platform, mlflowEnabled) are derived from PlatformContext.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	dscCtx *modules.DSCContext,
	cfg *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	if dscCtx == nil || dscCtx.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build Workbenches CR")
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(
		&dscCtx.DSC.Spec.Components.Workbenches.WorkbenchesCommonSpec,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to convert WorkbenchesCommonSpec to unstructured: %w", err)
	}
	spec["managementState"] = string(dscCtx.DSC.Spec.Components.Workbenches.ManagementState)
	spec["mlflowEnabled"] = dscCtx.DSC.Spec.Components.MLflowOperator.ManagementState == operatorv1.Managed

	if cfg != nil {
		spec["gatewayDomain"] = cfg.GatewayDomain
		spec["platform"] = workbenchesPlatformType(cfg.Release.Name)
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

// WriteLegacyStatusFields mirrors workbenchNamespace from the DSC spec into
// dsc.status.components.workbenches so odh-dashboard getWorkbenchNamespace() can
// resolve the legacy notebooks namespace without falling back to the applications
// namespace.
//
// TODO: Remove once dashboard reads workbenchNamespace directly from the Workbenches
// CR (workbenches-operator status) instead of DSC status.
func (h *handler) WriteLegacyStatusFields(
	_ context.Context,
	_ client.Client,
	dsc *dscv2.DataScienceCluster,
	enabled bool,
) error {
	if dsc == nil {
		return nil
	}

	if !enabled {
		writeDSCWorkbenchNamespace(dsc, false, "")
		return nil
	}

	writeDSCWorkbenchNamespace(dsc, true, dsc.Spec.Components.Workbenches.WorkbenchNamespace)
	return nil
}

func writeDSCWorkbenchNamespace(dsc *dscv2.DataScienceCluster, enabled bool, workbenchNamespace string) {
	if dsc == nil {
		return
	}

	if !enabled || workbenchNamespace == "" {
		if dsc.Status.Components.Workbenches.WorkbenchesCommonStatus != nil {
			dsc.Status.Components.Workbenches.WorkbenchNamespace = ""
		}
		return
	}

	if dsc.Status.Components.Workbenches.WorkbenchesCommonStatus == nil {
		dsc.Status.Components.Workbenches.WorkbenchesCommonStatus = &componentApi.WorkbenchesCommonStatus{}
	}

	dsc.Status.Components.Workbenches.WorkbenchNamespace = workbenchNamespace
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
