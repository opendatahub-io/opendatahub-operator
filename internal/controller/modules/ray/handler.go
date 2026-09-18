package ray

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
	moduleName = componentApi.RayComponentName
	crName     = componentApi.RayInstanceName

	deploymentName = "ray-module-operator-controller-manager"

	controllerImage = "RELATED_IMAGE_ODH_RAY_MODULE_OPERATOR_IMAGE"

	// Module manifests are under ray-module-operator/config. OpenShift
	// flavors use the openshift overlay (SCC, image patches, params.env).
	// xKS falls back to the kubebuilder default overlay.
	moduleSourcePath = "default"
)

var (
	sourcePathByPlatform = map[common.Platform]string{
		cluster.OpenDataHub:      "openshift",
		cluster.SelfManagedRhoai: "openshift",
		cluster.ManagedRhoai:     "openshift",
	}

	relatedImages = []string{
		"RELATED_IMAGE_ODH_KUBERAY_OPERATOR_CONTROLLER_IMAGE",
		"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
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
				ManifestDir:          "ray",
				SourcePath:           moduleSourcePath,
				SourcePathByPlatform: sourcePathByPlatform,
				ControllerImage:      controllerImage,
				RelatedImages:        relatedImages,
				DeploymentName:       deploymentName,
				GVK:                  gvk.Ray,
			},
		},
	}
}

func (h *handler) PopulatePlatformModule(pm *configv1alpha1.PlatformModules, dscCtx *modules.DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSC == nil {
		return
	}
	ms := dscCtx.DSC.Spec.Components.Ray.ManagementState
	if ms == "" {
		ms = operatorv1.Removed
	}
	pm.Ray.ManagementState = ms
}

func (h *handler) IsEnabled(modules *configv1alpha1.PlatformModules) bool {
	return modules != nil && modules.Ray.ManagementState == operatorv1.Managed
}

func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	dscCtx *modules.DSCContext,
	cfg *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	if dscCtx == nil || dscCtx.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build Ray CR")
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(
		&dscCtx.DSC.Spec.Components.Ray.RayCommonSpec,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to convert RayCommonSpec to unstructured: %w", err)
	}

	if cfg != nil {
		spec["applicationsNamespace"] = cfg.ApplicationsNamespace
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
