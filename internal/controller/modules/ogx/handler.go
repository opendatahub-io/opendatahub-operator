package ogx

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	moduleName = componentApi.OGXComponentName
	crName     = componentApi.OGXInstanceName

	manifestDir       = "ogx"
	deploymentName    = "opendatahub-ogx-operator"
	initContainerName = "copy-manifests"
	controllerImage   = "RELATED_IMAGE_ODH_OGX_MODULE_OPERATOR_IMAGE"
)

var (
	sourcePathByPlatform = map[common.Platform]string{
		cluster.OpenDataHub:      "overlays/odh",
		cluster.SelfManagedRhoai: "overlays/rhoai",
	}

	relatedImages = []string{
		"RELATED_IMAGE_ODH_OGX_K8S_OPERATOR_IMAGE",
		"RELATED_IMAGE_ODH_OGX_CORE_IMAGE",
	}
)

type handler struct {
	modules.BaseHandler
}

func NewHandler() *handler {
	return &handler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:                 moduleName,
				CRName:               crName,
				ManifestDir:          manifestDir,
				SourcePathByPlatform: sourcePathByPlatform,
				ControllerImage:      controllerImage,
				InitContainerName:    initContainerName,
				RelatedImages:        relatedImages,
				DeploymentName:       deploymentName,
				GVK:                  gvk.OGX,
			},
		},
	}
}

func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil {
		return false
	}
	if platform.DSC != nil {
		return platform.DSC.Spec.Components.OGX.ManagementState == operatorv1.Managed
	}
	if platform.Platform != nil {
		return platform.Platform.Spec.Modules.OGX.ManagementState == operatorv1.Managed
	}
	return false
}

func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform == nil {
		return nil, errors.New("platform context is nil, cannot build OGX CR")
	}

	if platform.DSC != nil &&
		platform.DSC.Spec.Components.LlamaStackOperator.ManagementState == operatorv1.Managed {
		return nil, fmt.Errorf(
			"LlamaStackOperator is set to %s; it has been deprecated, set it to %s before enabling OGX",
			operatorv1.Managed, operatorv1.Removed,
		)
	}

	var spec map[string]any

	switch {
	case platform.DSC != nil:
		var err error
		spec, err = runtime.DefaultUnstructuredConverter.ToUnstructured(
			&platform.DSC.Spec.Components.OGX.OGXCommonSpec,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to convert OGXCommonSpec to unstructured: %w", err)
		}
	case platform.Platform != nil:
		spec = map[string]any{
			"managementState": string(platform.Platform.Spec.Modules.OGX.ManagementState),
		}
	default:
		return nil, errors.New("neither DSC CR nor Platform CR exists, cannot build OGX CR")
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
