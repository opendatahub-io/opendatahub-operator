package trustyai

import (
	"context"
	"errors"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	moduleName = componentApi.TrustyAIComponentName
	crName     = componentApi.TrustyAIInstanceName

	deploymentName = "trustyai-operator-module-controller-manager"

	controllerImage = "RELATED_IMAGE_ODH_TRUSTYAI_MODULE_OPERATOR_IMAGE"

	// permitDeny matches the DSC / module CRD default for LMEval permit fields.
	permitDeny = "deny"
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
				ManifestDir:     "trustyai",
				ContextDir:      "default",
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
func (h *handler) PopulatePlatformModule(pm *configApi.PlatformModules, dscCtx *modules.DSCContext) {
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
func (h *handler) IsEnabled(modules *configApi.PlatformModules) bool {
	return modules != nil && modules.TrustyAI.ManagementState == operatorv1.Managed
}

// BuildModuleCR projects the DSC TrustyAI component spec onto the module CR.
// TrustyAI is DSC-mode only for now; Platform mode (xKS) is not yet supported.
//
// DSC and the TrustyAI module CRD both use the string enum allow/deny for
// eval.lmeval.permit* (restored for 3.5→3.6 upgrade compatibility). Values
// are projected as-is; empty defaults to "deny" to match the CRD default.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	dscCtx *modules.DSCContext,
	_ *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	if dscCtx == nil || dscCtx.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build TrustyAI CR")
	}

	lmeval := dscCtx.DSC.Spec.Components.TrustyAI.Eval.LMEval

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{
				"mcpGuardrailsMode": dscCtx.DSC.Spec.Components.TrustyAI.MCPGuardrailsMode,
				"eval": map[string]any{
					"lmeval": map[string]any{
						"permitCodeExecution": permitValue(lmeval.PermitCodeExecution),
						"permitOnline":        permitValue(lmeval.PermitOnline),
					},
				},
			},
		},
	}
	u.SetGroupVersionKind(h.Config.GVK)
	u.SetName(h.Config.CRName)

	return u, nil
}

// permitValue returns the DSC permit enum value, defaulting empty to "deny".
func permitValue(v string) string {
	if v == "" {
		return permitDeny
	}
	return v
}
