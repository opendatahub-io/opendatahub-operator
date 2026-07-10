package mcplifecycleoperator

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
)

const (
	moduleName = componentApi.MCPLifecycleOperatorComponentName
	crName     = componentApi.MCPLifecycleOperatorInstanceName

	// deploymentName is the rendered Deployment name after kustomize applies
	// namePrefix "mcp-lifecycle-module-operator-" to "controller-manager".
	deploymentName = "mcp-lifecycle-module-operator-controller-manager"
)

type handler struct {
	modules.BaseHandler
}

func NewHandler() *handler {
	return &handler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:            moduleName,
				CRName:          crName,
				ManifestDir:     "mcplifecycleoperator",
				ContextDir:      "default",
				DeploymentName:  deploymentName,
				GVK:             gvk.MCPLifecycleOperator,
				ControllerImage: "RELATED_IMAGE_ODH_MCP_LIFECYCLE_MODULE_OPERATOR_IMAGE",
				RelatedImages: []string{
					"RELATED_IMAGE_ODH_MCP_LIFECYCLE_OPERATOR_IMAGE",
				},
			},
		},
	}
}

// IsEnabled checks whether the MCPLifecycleOperator module should be deployed.
// In DSC mode, reads DSC.Spec.Components.MCPLifecycleOperator.ManagementState.
// In Platform mode (xKS), reads Platform.Spec.Modules.MCPLifecycleOperator.ManagementState.
func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil {
		return false
	}
	if platform.DSC != nil {
		return platform.DSC.Spec.Components.MCPLifecycleOperator.ManagementState == operatorv1.Managed
	}
	if platform.Platform != nil {
		return platform.Platform.Spec.Modules.MCPLifecycleOperator.ManagementState == operatorv1.Managed
	}
	return false
}

// BuildModuleCR projects platform configuration onto the module CR.
// In DSC mode, projects the component stanza from the DSC.
// In Platform mode (xKS), projects managementState from the Platform CR.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform == nil {
		return nil, errors.New("platform context is nil, cannot build MCPLifecycleOperator CR")
	}

	var spec map[string]any

	switch {
	case platform.DSC != nil:
		var err error
		spec, err = runtime.DefaultUnstructuredConverter.ToUnstructured(
			&platform.DSC.Spec.Components.MCPLifecycleOperator.MCPLifecycleOperatorCommonSpec,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to convert MCPLifecycleOperatorCommonSpec to unstructured: %w", err)
		}
	case platform.Platform != nil:
		spec = map[string]any{
			"managementState": string(platform.Platform.Spec.Modules.MCPLifecycleOperator.ManagementState),
		}
	default:
		return nil, errors.New("neither DSC nor Platform is available, cannot build MCPLifecycleOperator CR")
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
