package dashboard

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
	moduleName = componentApi.DashboardComponentName
	// crName must match dashboard-operator CRD CEL (default-dashboard); see odh-dashboard#8093.
	crName = componentApi.DashboardInstanceName
)

type handler struct {
	modules.BaseHandler
}

func NewHandler() *handler {
	return &handler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:              moduleName,
				CRName:            crName,
				ReleaseName:       "dashboard-operator",
				ChartDir:          "dashboard-operator",
				NamespaceValueKey: "namespace",
				Values: map[string]any{
					// Chart defaults namePrefix to "odh-", producing Deployment
					// "odh-dashboard-operator". Clear it so the Deployment name matches
					// ReleaseName for module env injection (deploymentNameFromManifests).
					"namePrefix": "",
					// Disable webhook until chart defaults are updated upstream.
					// CEL on the CRD enforces the singleton constraint.
					"webhook": map[string]any{"enabled": false},
					// Override chart :main tag defaults with empty strings so the
					// dashboard-operator falls back to digest-pinned params.env defaults.
					"relatedImages": emptyRelatedImageValues(),
				},
				GVK:             gvk.Dashboard, // components.platform.opendatahub.io/v1alpha1/Dashboard
				ControllerImage: "RELATED_IMAGE_ODH_DASHBOARD_OPERATOR_IMAGE",
				RelatedImages:   relatedImages(),
			},
		},
	}
}

func (h *handler) PopulatePlatformModule(pm *configv1alpha1.PlatformModules, dscCtx *modules.DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSC == nil {
		return
	}
	ms := dscCtx.DSC.Spec.Components.Dashboard.ManagementState
	if ms == "" {
		ms = operatorv1.Removed
	}
	pm.Dashboard.ManagementState = ms
}

// IsEnabled checks whether the dashboard module should be deployed based on
// PlatformModules.Dashboard.ManagementState.
func (h *handler) IsEnabled(modules *configv1alpha1.PlatformModules) bool {
	return modules != nil && modules.Dashboard.ManagementState == operatorv1.Managed
}

// BuildModuleCR projects user-facing DSC dashboard configuration and platform
// fields from DSCContext and ModuleCRConfig onto the module CR.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	dscCtx *modules.DSCContext,
	cfg *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	if dscCtx == nil {
		return nil, errors.New("DSC context is nil, cannot build dashboard CR")
	}
	if dscCtx.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build dashboard CR")
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&dscCtx.DSC.Spec.Components.Dashboard)
	if err != nil {
		return nil, fmt.Errorf("failed to convert DSCDashboard to unstructured: %w", err)
	}

	spec["deploymentMode"] = "Standalone"

	spec["components"] = buildComponentsMap(dscCtx)

	if cfg != nil && cfg.GatewayDomain != "" {
		spec["gateway"] = map[string]any{
			"domain": cfg.GatewayDomain,
		}
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

// buildComponentsMap projects the management state of DSC components
// referenced by dashboard-operator modules onto the Dashboard CR.
func buildComponentsMap(dscCtx *modules.DSCContext) map[string]any {
	c := &dscCtx.DSC.Spec.Components

	refs := []struct {
		name  string
		state operatorv1.ManagementState
	}{
		{"modelregistry", c.ModelRegistry.ManagementState},
		{"mlflowoperator", c.MLflowOperator.ManagementState},
		{"trustyai", c.TrustyAI.ManagementState},
		{"aipipelines", c.AIPipelines.ManagementState},
	}

	result := make(map[string]any, len(refs))
	for _, ref := range refs {
		state := string(ref.state)
		if state == "" {
			state = string(operatorv1.Removed)
		}

		result[ref.name] = map[string]any{
			"managementState": state,
		}
	}

	return result
}
