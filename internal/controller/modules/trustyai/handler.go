package trustyai

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
	moduleName = componentApi.TrustyAIKind
	crName     = "trustyai"
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
				ReleaseName:       "trustyai-operator",
				ChartDir:          "trustyai-operator",
				NamespaceValueKey: "operatorNamespace",
				GVK:               gvk.TrustyAI,
				RelatedImages: []string{
					"RELATED_IMAGE_TRUSTYAI_SERVICE",
					"RELATED_IMAGE_TRUSTYAI_SERVICE_OPERATOR",
				},
			},
		},
	}
}

// IsEnabled checks whether the TrustyAI module should be deployed.
// In DSC mode, reads DSC.Spec.Components.TrustyAI.ManagementState.
// In Platform mode (xKS), TrustyAI is not yet supported.
func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil {
		return false
	}
	if platform.DSC != nil {
		return platform.DSC.Spec.Components.TrustyAI.ManagementState == operatorv1.Managed
	}
	// TrustyAI module not yet available in Platform mode
	return false
}

// BuildModuleCR projects platform TrustyAI configuration onto the module CR.
// In DSC mode, the full DSC TrustyAI component spec is converted directly.
// In Platform mode, TrustyAI is not yet supported.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform == nil {
		return nil, errors.New("platform context is nil, cannot build TrustyAI CR")
	}

	var spec map[string]any

	switch {
	case platform.DSC != nil:
		var err error
		spec, err = runtime.DefaultUnstructuredConverter.ToUnstructured(&platform.DSC.Spec.Components.TrustyAI)
		if err != nil {
			return nil, fmt.Errorf("failed to convert TrustyAI component to unstructured: %w", err)
		}
	case platform.Platform != nil:
		// TrustyAI module not yet available in Platform mode
		return nil, errors.New("TrustyAI module is not supported in Platform mode yet")
	default:
		return nil, errors.New("neither DSC nor Platform is available, cannot build TrustyAI CR")
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
