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
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
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

func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil || platform.Platform == nil {
		return false
	}
	return platform.Platform.Spec.Modules.MCPLifecycleOperator.ManagementState == operatorv1.Managed
}

// BuildModuleCR constructs the MCPLifecycleOperator CR from DSC spec.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	dsc *dscv2.DataScienceCluster,
	_ *dsciv2.DSCInitialization,
) (*unstructured.Unstructured, error) {
	if dsc == nil {
		return nil, errors.New("DSC is nil, cannot build MCPLifecycleOperator CR")
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(
		&dsc.Spec.Components.MCPLifecycleOperator.MCPLifecycleOperatorCommonSpec,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to convert MCPLifecycleOperatorCommonSpec to unstructured: %w", err)
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
