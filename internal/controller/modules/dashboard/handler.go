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
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	moduleName = componentApi.DashboardComponentName
	// crName must match dashboard-operator CRD CEL (default-dashboard); see odh-dashboard#8093.
	crName = componentApi.DashboardInstanceName

	// dashboardAIPipelinesName is the key used in the Dashboard CR components map for
	// AI Pipelines. Keep in sync with dashboard-operator's component name for DSP.
	dashboardAIPipelinesName = "aipipelines"
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
// When dscCtx or dscCtx.DSC is nil, returns an error.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	dscCtx *modules.DSCContext,
	cfg *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	if dscCtx == nil || dscCtx.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build Dashboard CR")
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&dscCtx.DSC.Spec.Components.Dashboard)
	if err != nil {
		return nil, fmt.Errorf("failed to convert DSCDashboard to unstructured: %w", err)
	}
	spec["components"] = buildComponentsMap(dscCtx)
	if ns := resolveNotebooksNamespace(dscCtx, cfg); ns != "" {
		spec["notebooksNamespace"] = ns
	}
	if ns := resolveModelRegistryNamespace(dscCtx); ns != "" {
		spec["modelRegistryNamespace"] = ns
	}

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
		{componentApi.ModelRegistryComponentName, c.ModelRegistry.ManagementState},
		{componentApi.MLflowOperatorComponentName, c.MLflowOperator.ManagementState},
		{componentApi.TrustyAIComponentName, c.TrustyAI.ManagementState},
		{dashboardAIPipelinesName, c.AIPipelines.ManagementState},
	}

	result := make(map[string]any, len(refs))
	for _, ref := range refs {
		result[ref.name] = map[string]any{
			"managementState": string(components.NormalizeManagementState(ref.state)),
		}
	}

	return result
}

// resolveNotebooksNamespace returns the notebooks namespace to project into the Dashboard CR.
// Returns empty string if workbenches is not Managed, so the dashboard-operator skips
// cross-namespace RBAC for notebooks when the component is absent.
func resolveNotebooksNamespace(dscCtx *modules.DSCContext, cfg *modules.ModuleCRConfig) string {
	if dscCtx == nil || dscCtx.DSC == nil {
		return ""
	}
	if dscCtx.DSC.Spec.Components.Workbenches.ManagementState != operatorv1.Managed {
		return ""
	}
	if ns := dscCtx.DSC.Spec.Components.Workbenches.WorkbenchNamespace; ns != "" {
		return ns
	}
	if cfg != nil {
		switch cfg.Release.Name {
		case cluster.SelfManagedRhoai, cluster.ManagedRhoai:
			return cluster.DefaultNotebooksNamespaceRHOAI
		}
	}
	return cluster.DefaultNotebooksNamespaceODH
}

// resolveModelRegistryNamespace returns the model-registry namespace to project into the
// Dashboard CR. Returns empty string if ModelRegistry is not Managed.
func resolveModelRegistryNamespace(dscCtx *modules.DSCContext) string {
	if dscCtx == nil || dscCtx.DSC == nil {
		return ""
	}
	if dscCtx.DSC.Spec.Components.ModelRegistry.ManagementState != operatorv1.Managed {
		return ""
	}
	return dscCtx.DSC.Spec.Components.ModelRegistry.RegistriesNamespace
}
