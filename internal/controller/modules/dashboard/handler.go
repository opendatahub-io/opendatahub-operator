package dashboard

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
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

	// maasPortalField is the canonical Dashboard CR spec key for the MaaS
	// Portal management state, projected from the DSC v3 dashboard component.
	maasPortalField = "maasPortal"
	// maasConsumerPortalField is the deprecated Dashboard CR spec key kept for
	// legacy-only configurations until the canonical field is applied.
	maasConsumerPortalField = "maasConsumerPortal"
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
				SubmoduleConditions: []modules.SubmoduleCondition{
					{
						// Aligned with dashboard-operator's MaaSPortalAvailable condition.
						SourceConditionType: "MaaSPortalAvailable",
						DSCConditionType:    "MaaSPortalAvailable",
						StatusFieldName:     "MaaSPortal",
						IsEnabled: func(dscCtx *modules.DSCContext) bool {
							if dscCtx == nil || dscCtx.DSC == nil {
								return false
							}
							return dscCtx.DSC.Spec.Components.Dashboard.MaaSPortal.ManagementState == operatorv1.Managed
						},
					},
				},
			},
		},
	}
}

func (h *handler) PopulatePlatformModule(pm *configApi.PlatformModules, dscCtx *modules.DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSC == nil {
		return
	}
	dashboard := dscCtx.DSC.Spec.Components.Dashboard
	// The dashboard-operator Deployment ships both the core Dashboard and the
	// MaaS Consumer Portal submodule, so it must stay up while either workload
	// is Managed. PlatformModules.Dashboard has no portal field, so the compound
	// OR is computed here; IsEnabled() reads the already-OR'd state.
	switch {
	case dashboard.Standard.ManagementState == operatorv1.Managed:
		pm.Dashboard.ManagementState = operatorv1.Managed
	case dashboard.MaaSPortal.ManagementState == operatorv1.Managed:
		pm.Dashboard.ManagementState = operatorv1.Managed
	default:
		pm.Dashboard.ManagementState = operatorv1.Removed
	}
}

// IsEnabled checks whether the dashboard module should be deployed based on
// PlatformModules.Dashboard.ManagementState.
func (h *handler) IsEnabled(modules *configApi.PlatformModules) bool {
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

	spec := dashboardModuleSpec(dscCtx.DSC.Spec.Components.Dashboard)
	spec["components"] = buildComponentsMap(dscCtx)
	if ns := resolveNotebooksNamespace(dscCtx, cfg); ns != "" {
		spec["notebooksNamespace"] = ns
	}
	if ns := resolveAIHubNamespace(dscCtx); ns != "" {
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

func dashboardModuleSpec(dashboard componentApi.DSCDashboard) map[string]any {
	spec := make(map[string]any, 2)

	switch dashboard.Standard.ManagementState {
	case operatorv1.Managed:
		spec["managementState"] = string(operatorv1.Managed)
	default:
		spec["managementState"] = string(operatorv1.Removed)
	}

	switch dashboard.MaaSPortal.ManagementState {
	case operatorv1.Managed:
		spec["maasPortal"] = map[string]any{
			"managementState": string(operatorv1.Managed),
		}
	default:
		spec["maasPortal"] = map[string]any{
			"managementState": string(operatorv1.Removed),
		}
	}

	return spec
}

// buildComponentsMap projects the management state of DSC components
// referenced by dashboard-operator modules onto the Dashboard CR.
func buildComponentsMap(dscCtx *modules.DSCContext) map[string]any {
	c := &dscCtx.DSC.Spec.Components

	refs := []struct {
		name  string
		state operatorv1.ManagementState
	}{
		{"modelregistry", c.AIHub.ManagementState},
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

// resolveAIHubNamespace returns the AI Hub namespace to project into the Dashboard CR.
func resolveAIHubNamespace(dscCtx *modules.DSCContext) string {
	if dscCtx == nil || dscCtx.DSC == nil {
		return ""
	}
	if dscCtx.DSC.Spec.Components.AIHub.ManagementState != operatorv1.Managed {
		return ""
	}
	return dscCtx.DSC.Spec.Components.AIHub.InstancesNamespace
}

// maasConsumerPortalPatch removes the deprecated spec.maasConsumerPortal field
// from the Dashboard CR. A JSON merge patch with a null value deletes the
// stored key; omitting the field from the applied configuration would not.
var maasConsumerPortalPatch = []byte(`{"spec":{"maasConsumerPortal":null}}`)

// CleanupDeprecatedModuleCRFields removes the deprecated spec.maasConsumerPortal
// field from the live Dashboard CR once the canonical spec.maasPortal field has
// been applied. The dashboard-operator gives maasPortal precedence, so the
// stale legacy value never changes behavior, but leaving it in place makes the
// CR misleading and complicates troubleshooting (RHOAIENG-100941).
//
// The cleanup is guarded and idempotent:
//   - a missing CR (module not deployed or CRD absent) is not an error;
//   - a CR being deleted is left for the deletion flow;
//   - legacy-only configurations are preserved until spec.maasPortal is present;
//   - once the legacy field is absent, no further writes are issued.
func (h *handler) CleanupDeprecatedModuleCRFields(ctx context.Context, cli client.Client) error {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(h.Config.GVK)

	if err := cli.Get(ctx, client.ObjectKey{Name: h.Config.CRName}, u); err != nil {
		if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
			return nil
		}
		return fmt.Errorf("getting Dashboard CR %s: %w", h.Config.CRName, err)
	}

	if !u.GetDeletionTimestamp().IsZero() {
		return nil
	}

	spec, ok := u.Object["spec"].(map[string]any)
	if !ok {
		return nil
	}

	// Only clean up once the canonical field is present, so legacy-only
	// configurations keep working until the operator writes maasPortal.
	if _, canonical := spec[maasPortalField]; !canonical {
		return nil
	}
	if _, legacy := spec[maasConsumerPortalField]; !legacy {
		return nil
	}

	if err := cli.Patch(ctx, u, client.RawPatch(k8stypes.MergePatchType, maasConsumerPortalPatch)); err != nil {
		return fmt.Errorf("removing deprecated %s from Dashboard CR %s: %w", maasConsumerPortalField, h.Config.CRName, err)
	}

	logf.FromContext(ctx).Info("removed deprecated field from Dashboard CR",
		"module", h.Config.Name,
		"name", h.Config.CRName,
		"field", maasConsumerPortalField)

	return nil
}
