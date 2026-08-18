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
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
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

func (h *handler) PopulatePlatformModule(pm *configv1alpha1.PlatformModules, dscCtx *modules.DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSC == nil {
		return
	}
	ms := dscCtx.DSC.Spec.Components.OGX.ManagementState
	if ms == "" {
		ms = operatorv1.Removed
	}
	pm.OGX.ManagementState = ms
}

func (h *handler) IsEnabled(modules *configv1alpha1.PlatformModules) bool {
	return modules != nil && modules.OGX.ManagementState == operatorv1.Managed
}

func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	dscCtx *modules.DSCContext,
	_ *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	if dscCtx == nil || dscCtx.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build OGX CR")
	}

	if dscCtx.DSC.Spec.Components.LlamaStackOperator.ManagementState == operatorv1.Managed {
		return nil, fmt.Errorf(
			"LlamaStackOperator is set to %s; it has been deprecated, set it to %s before enabling OGX",
			operatorv1.Managed, operatorv1.Removed,
		)
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(
		&dscCtx.DSC.Spec.Components.OGX.OGXCommonSpec,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to convert OGXCommonSpec to unstructured: %w", err)
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
