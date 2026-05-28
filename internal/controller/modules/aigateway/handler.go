package aigateway

import (
	"context"
	"errors"
	"os"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
)

const (
	moduleName = componentApi.AIGatewayComponentName
	crName     = componentApi.AIGatewayInstanceName
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
				ReleaseName:       "opendatahub-ai-gateway-operator",
				ChartDir:          "ai-gateway-operator",
				NamespaceValueKey: "operatorNamespace",
				GVK: schema.GroupVersionKind{
					Group:   "components.platform.opendatahub.io",
					Version: "v1alpha1",
					Kind:    componentApi.AIGatewayKind,
				},
				RelatedImages: []string{
					"RELATED_IMAGE_ODH_AI_GATEWAY_OPERATOR_IMAGE",
					"RELATED_IMAGE_ODH_BATCH_GATEWAY_OPERATOR_IMAGE",
					"RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_APISERVER_IMAGE",
					"RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_PROCESSOR_IMAGE",
					"RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_GC_IMAGE",
				},
			},
		},
	}
}

// GetOperatorManifests overrides BaseHandler to inject the ai-gateway-operator
// image from RELATED_IMAGE_ODH_AI_GATEWAY_OPERATOR_IMAGE into the Helm chart's
// image.fullRef value. This is necessary because the Helm chart bakes the image
// at render time, and injectModuleEnv only adds env vars to the container — it
// does not modify the Deployment's image field.
func (h *handler) GetOperatorManifests(platform *modules.PlatformContext) modules.OperatorManifests {
	if img := os.Getenv("RELATED_IMAGE_ODH_AI_GATEWAY_OPERATOR_IMAGE"); img != "" {
		h.Config.Values = map[string]any{
			"image": map[string]any{
				"fullRef": img,
			},
		}
	}
	return h.BaseHandler.GetOperatorManifests(platform)
}

func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil {
		return false
	}
	if platform.DSC != nil {
		return platform.DSC.Spec.Components.AIGateway.ManagementState == operatorv1.Managed
	}
	return false
}

func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform == nil || platform.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build AIGateway CR")
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&platform.DSC.Spec.Components.AIGateway.AIGatewayCommonSpec)
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
