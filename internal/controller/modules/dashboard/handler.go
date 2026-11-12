package dashboard

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

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

	namespacedRBACOptInLabel = "dashboard.opendatahub.io/allow-namespaced-rbac"
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
					"webhook": map[string]any{
						"enabled":               true,
						"namespacedRBACWriters": namespacedRBACWriterUsernames(),
						"certManager":           map[string]any{"enabled": true},
					},
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

func namespacedRBACWriterUsernames() []string {
	return []string{
		"system:serviceaccount:redhat-ods-operator:redhat-ods-operator-controller-manager",
		"system:serviceaccount:opendatahub-operator:opendatahub-operator-controller-manager",
		"system:serviceaccount:openshift-operators:opendatahub-operator-controller-manager",
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
	ctx context.Context,
	cli client.Client,
	dscCtx *modules.DSCContext,
	cfg *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	if dscCtx == nil || dscCtx.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build Dashboard CR")
	}

	spec := dashboardModuleSpec(dscCtx.DSC.Spec.Components.Dashboard)
	spec["components"] = buildComponentsMap(dscCtx)
	notebooksNamespace := resolveNotebooksNamespace(dscCtx, cfg)
	modelRegistryNamespace := resolveAIHubNamespace(dscCtx)
	if err := ensureNamespacedRBACOptIn(ctx, cli, notebooksNamespace, modelRegistryNamespace); err != nil {
		return nil, err
	}
	if notebooksNamespace != "" {
		spec["notebooksNamespace"] = notebooksNamespace
	}
	if modelRegistryNamespace != "" {
		spec["modelRegistryNamespace"] = modelRegistryNamespace
	}

	if cfg != nil && cfg.GatewayConfig != nil && cfg.GatewayConfig.Status.Domain != "" {
		spec["gateway"] = map[string]any{
			"domain": cfg.GatewayConfig.Status.Domain,
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

// ensureNamespacedRBACOptIn records explicit consent on existing namespaces
// before projecting them into the Dashboard CR. Component operators may create
// their namespaces asynchronously, so a missing namespace is left fail-closed
// for the dashboard-operator and will be labelled on a later DSC reconcile.
func ensureNamespacedRBACOptIn(ctx context.Context, cli client.Client, namespaces ...string) error {
	seen := make(map[string]struct{}, len(namespaces))
	for _, name := range namespaces {
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		if cli == nil {
			return errors.New("client is required to opt in namespaced RBAC targets")
		}

		namespace := &corev1.Namespace{}
		if err := cli.Get(ctx, client.ObjectKey{Name: name}, namespace); err != nil {
			if k8serr.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("getting namespaced RBAC target namespace %s: %w", name, err)
		}
		if namespace.Labels[namespacedRBACOptInLabel] == "true" {
			continue
		}

		before := namespace.DeepCopy()
		if namespace.Labels == nil {
			namespace.Labels = map[string]string{}
		}
		namespace.Labels[namespacedRBACOptInLabel] = "true"
		if err := cli.Patch(ctx, namespace, client.MergeFrom(before)); err != nil {
			return fmt.Errorf("opting namespace %s in to dashboard namespaced RBAC: %w", name, err)
		}
	}
	return nil
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
		{componentApi.DataRegistryComponentName, c.Data.DataRegistry.ManagementState},
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
