package modelsasservice

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
	// ModuleName is the name of the ModelsAsService module.
	ModuleName = componentApi.ModelsAsServiceComponentName
	moduleName = ModuleName
	// CRName is the default name of the ModelsAsService CR.
	CRName = componentApi.ModelsAsServiceInstanceName
	crName = CRName
)

type handler struct {
	modules.BaseHandler
}

func NewHandler() *handler {
	return &handler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:        moduleName,
				CRName:      crName,
				ReleaseName: "maas-controller",
				ChartDir:    "maas",
				GVK:         gvk.ModelsAsService,
				RelatedImages: []string{
					"RELATED_IMAGE_ODH_MAAS_CONTROLLER_IMAGE",
					"RELATED_IMAGE_ODH_MAAS_API_IMAGE",
					"RELATED_IMAGE_ODH_AI_GATEWAY_PAYLOAD_PROCESSING_IMAGE",
					"RELATED_IMAGE_UBI_MINIMAL_IMAGE",
				},
			},
		},
	}
}

// IsEnabled checks whether the ModelsAsService module should be deployed.
// MaaS is a KServe sub-component, so it requires:
// 1. KServe.ManagementState == Managed
// 2. KServe.ModelsAsService.ManagementState == Managed
//
// In DSC mode (DSCI present), reads DSC.Spec.Components.Kserve.
// In Platform mode (xKS), returns true (platform-managed).
func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil {
		return false
	}

	// Platform mode (xKS): MaaS enabled by platform config
	if platform.Platform != nil {
		return true
	}

	// DSC mode: check KServe and ModelsAsService management states
	if platform.DSC != nil {
		if platform.DSC.Spec.Components.Kserve.ManagementState != operatorv1.Managed {
			return false
		}
		return platform.DSC.Spec.Components.Kserve.ModelsAsService.ManagementState == operatorv1.Managed
	}

	return false
}

// BuildModuleCR projects platform ModelsAsService configuration onto the module CR.
//
// In DSC mode:
// - Converts DSCModelsAsServiceSpec directly to ModelsAsServiceSpec
// - Includes ManagementState from DSC.Spec.Components.Kserve.ModelsAsService
//
// In Platform mode:
// - Projects minimal spec with ManagementState: Managed.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform == nil {
		return nil, errors.New("platform context is nil, cannot build ModelsAsService CR")
	}

	var spec map[string]any

	switch {
	case platform.DSC != nil:
		var err error
		spec, err = runtime.DefaultUnstructuredConverter.ToUnstructured(&platform.DSC.Spec.Components.Kserve.ModelsAsService)
		if err != nil {
			return nil, fmt.Errorf("failed to convert DSCModelsAsServiceSpec to unstructured: %w", err)
		}
	case platform.Platform != nil:
		// Platform mode: minimal spec
		spec = map[string]any{
			"managementState": string(operatorv1.Managed),
		}
	default:
		return nil, errors.New("neither DSC nor Platform is available, cannot build ModelsAsService CR")
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
