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
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
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

// IsEnabled checks whether the dashboard module should be deployed based on
// DSC.Spec.Components.Dashboard.ManagementState.
func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil || platform.DSC == nil {
		return false
	}
	return platform.DSC.Spec.Components.Dashboard.ManagementState == operatorv1.Managed
}

// BuildModuleCR projects user-facing DSC dashboard configuration and platform
// fields from PlatformContext onto the module CR.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform == nil {
		return nil, errors.New("platform context is nil, cannot build dashboard CR")
	}
	if platform.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build dashboard CR")
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&platform.DSC.Spec.Components.Dashboard)
	if err != nil {
		return nil, fmt.Errorf("failed to convert DSCDashboard to unstructured: %w", err)
	}

	spec["deploymentMode"] = "Standalone"

	spec["components"] = buildComponentsMap(platform)

	if platform.GatewayDomain != "" {
		spec["gateway"] = map[string]any{
			"domain": platform.GatewayDomain,
		}
	}

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": spec,
		},
	}
	u.SetGroupVersionKind(h.Config.GVK)
	u.SetName(h.Config.CRName)
	u.SetAnnotations(map[string]string{
		annotations.ManagementStateAnnotation: string(platform.DSC.Spec.Components.Dashboard.ManagementState),
	})

	return u, nil
}

// buildComponentsMap projects the management state of DSC components
// referenced by dashboard-operator modules onto the Dashboard CR.
func buildComponentsMap(platform *modules.PlatformContext) map[string]any {
	c := &platform.DSC.Spec.Components

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
