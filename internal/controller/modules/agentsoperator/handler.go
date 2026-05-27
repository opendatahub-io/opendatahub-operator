package agentsoperator

import (
	"context"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
)

const (
	moduleName = componentApi.AgentsOperatorComponentName
	crName     = componentApi.AgentsOperatorInstanceName
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
				ReleaseName:       "agents-operator",
				ChartDir:          "agents-operator",
				NamespaceValueKey: "operatorNamespace",
				GVK: schema.GroupVersionKind{
					Group:   componentApi.GroupVersion.Group,
					Version: componentApi.GroupVersion.Version,
					Kind:    componentApi.AgentsOperatorKind,
				},
				RelatedImages: []string{
					"RELATED_IMAGE_ODH_AGENTS_OPERATOR_IMAGE",
				},
				// Bootstrap install: platform supplies namespace and manages certs; chart
				// must only emit operator RBAC + controller (see chart_compliance_test).
				Values: map[string]any{
					"certmanager": map[string]any{"enable": false},
					"webhook":     map[string]any{"enable": false},
					"metrics":     map[string]any{"enable": false},
				},
			},
		},
	}
}

// IsEnabled returns true when the Agents Operator component is managed.
// In DSC mode it reads the DSC component stanza; in Platform mode (xKS)
// the registry has already filtered by spec.modules so we return true.
func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil {
		return false
	}
	if platform.DSC == nil {
		return true
	}
	return platform.DSC.Spec.Components.AgentsOperator.ManagementState == operatorv1.Managed
}

// BuildModuleCR projects platform configuration onto the module CR.
// In DSC mode, fields are read from the DSC component stanza.
// In Platform mode (xKS), defaults to Managed since the registry
// already confirmed enablement.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	spec := map[string]any{
		"managementState": string(operatorv1.Managed),
	}

	if platform != nil && platform.DSC != nil {
		dsc := platform.DSC.Spec.Components.AgentsOperator
		spec["managementState"] = string(dsc.ManagementState)
		if dsc.Auth != nil {
			audiences := make([]any, len(dsc.Auth.Audiences))
			for i, a := range dsc.Auth.Audiences {
				audiences[i] = a
			}
			spec["auth"] = map[string]any{
				"enabled":   dsc.Auth.Enabled,
				"audiences": audiences,
			}
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
