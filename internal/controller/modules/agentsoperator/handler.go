package agentsoperator

import (
	"context"
	"errors"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
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

// IsEnabled returns true when the Agents Operator component is managed in the DSC.
func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil || platform.DSC == nil {
		return false
	}
	return platform.DSC.Spec.Components.AgentsOperator.ManagementState == operatorv1.Managed
}

// BuildModuleCR projects DSC Agents Operator configuration onto the module CR.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform == nil || platform.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build AgentsOperator CR")
	}

	dscAgents := platform.DSC.Spec.Components.AgentsOperator
	if dscAgents.ManagementState == "" {
		dscAgents.ManagementState = operatorv1.Managed
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&dscAgents)
	if err != nil {
		return nil, err
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
