package trustyai

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
)

const (
	moduleName = componentApi.TrustyAIComponentName
	crName     = componentApi.TrustyAIInstanceName

	deploymentName = "trustyai-operator-module-controller-manager"

	controllerImage = "RELATED_IMAGE_ODH_TRUSTYAI_MODULE_OPERATOR_IMAGE"
)

// relatedImages lists the operand images the TrustyAI module operator needs
// to create its managed workloads (TAS, LMES, EvalHub, GORCH, NemoGuardrails).
var relatedImages = []string{
	"RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_PY_IMAGE",
	"RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_OPERATOR_IMAGE",
	"RELATED_IMAGE_ODH_TA_LMES_DRIVER_IMAGE",
	"RELATED_IMAGE_ODH_TA_LMES_JOB_IMAGE",
	"RELATED_IMAGE_ODH_FMS_GUARDRAILS_ORCHESTRATOR_IMAGE",
	"RELATED_IMAGE_ODH_TRUSTYAI_VLLM_ORCHESTRATOR_GATEWAY_IMAGE",
	"RELATED_IMAGE_ODH_BUILT_IN_DETECTOR_IMAGE",
	"RELATED_IMAGE_ODH_PYTHON_312_IMAGE",
	"RELATED_IMAGE_ODH_TRUSTYAI_GARAK_LLS_PROVIDER_DSP_IMAGE",
	"RELATED_IMAGE_ODH_TRUSTYAI_NEMO_GUARDRAILS_SERVER_IMAGE",
	"RELATED_IMAGE_ODH_EVAL_HUB_IMAGE",
	"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
}

type handler struct {
	modules.BaseHandler
}

func NewHandler() *handler {
	return &handler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:            moduleName,
				CRName:          crName,
				GVK:             gvk.TrustyAI,
				ManifestDir:     "trustyai-module-operator",
				DeploymentName:  deploymentName,
				ControllerImage: controllerImage,
				RelatedImages:   relatedImages,
			},
		},
	}
}

// PopulatePlatformModule sets the TrustyAI module's management state on the
// PlatformModules struct, derived from the DSC spec. TrustyAI is DSC-mode
// only for now; Platform mode (xKS) is not yet supported.
func (h *handler) PopulatePlatformModule(pm *configv1alpha1.PlatformModules, dscCtx *modules.DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSC == nil {
		return
	}
	ms := dscCtx.DSC.Spec.Components.TrustyAI.ManagementState
	if ms == "" {
		ms = operatorv1.Removed
	}
	pm.TrustyAI.ManagementState = ms
}

// IsEnabled checks whether the TrustyAI module should be deployed, based on
// the projected PlatformModules state (itself derived from the DSC spec via
// PopulatePlatformModule).
func (h *handler) IsEnabled(modules *configv1alpha1.PlatformModules) bool {
	return modules != nil && modules.TrustyAI.ManagementState == operatorv1.Managed
}

// BuildModuleCR projects the DSC TrustyAI component spec onto the module CR.
// TrustyAI is DSC-mode only for now; Platform mode (xKS) is not yet supported.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	dscCtx *modules.DSCContext,
	_ *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	if dscCtx == nil || dscCtx.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build TrustyAI CR")
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(
		&dscCtx.DSC.Spec.Components.TrustyAI.TrustyAICommonSpec,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to convert TrustyAICommonSpec to unstructured: %w", err)
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
