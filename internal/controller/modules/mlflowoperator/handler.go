package mlflowoperator

import (
	"context"
	"errors"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	moduleName             = componentApi.MLflowOperatorComponentName
	crName                 = componentApi.MLflowOperatorInstanceName
	defaultGatewayName     = "data-science-gateway"
	rhoaiSectionTitle      = "OpenShift Self Managed Services"
	odhSectionTitle        = "OpenShift Open Data Hub"
	rhoaiPlatformOverlay   = "overlays/rhoai"
	opendatahubOverlayPath = "overlays/odh"
)

var (
	sectionTitles = map[common.Platform]string{
		cluster.ManagedRhoai:     rhoaiSectionTitle,
		cluster.SelfManagedRhoai: rhoaiSectionTitle,
		cluster.OpenDataHub:      odhSectionTitle,
	}
	overlayByPlatform = map[common.Platform]string{
		cluster.ManagedRhoai:     rhoaiPlatformOverlay,
		cluster.SelfManagedRhoai: rhoaiPlatformOverlay,
		cluster.OpenDataHub:      opendatahubOverlayPath,
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
				ManifestDir:          moduleName,
				SourcePath:           overlayByPlatform[cluster.OpenDataHub],
				SourcePathByPlatform: overlayByPlatform,
				DeploymentName:       "mlflow-operator-controller-manager",
				GVK:                  gvk.MLflowOperator,
				ControllerImage:      "RELATED_IMAGE_ODH_MLFLOW_OPERATOR_IMAGE",
				RelatedImages: []string{
					"RELATED_IMAGE_ODH_MLFLOW_IMAGE",
					"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
				},
				ExtraEnv: map[string]string{
					"ENABLE_MLFLOW_OPERATOR_MODULE_CONTROLLER": "true",
				},
			},
		},
	}
}

func (h *handler) PopulatePlatformModule(pm *configv1alpha1.PlatformModules, dscCtx *modules.DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSC == nil {
		return
	}
	ms := dscCtx.DSC.Spec.Components.MLflowOperator.ManagementState
	if ms == "" {
		ms = operatorv1.Removed
	}
	pm.MLflowOperator.ManagementState = ms
}

func (h *handler) IsEnabled(modules *configv1alpha1.PlatformModules) bool {
	return modules != nil && modules.MLflowOperator.ManagementState == operatorv1.Managed
}

func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	dscCtx *modules.DSCContext,
	cfg *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	if dscCtx == nil || dscCtx.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build MLflowOperator CR")
	}

	managementState := components.NormalizeManagementState(dscCtx.DSC.Spec.Components.MLflowOperator.ManagementState)

	// APPLICATIONS_NAMESPACE is injected directly into the operator Deployment so
	// the mlflow-operator process, cache, and namespaced RBAC all agree on one
	// startup-scoped target namespace.
	spec := map[string]any{
		"gatewayName": defaultGatewayName,
	}
	if cfg != nil {
		spec["sectionTitle"] = sectionTitle(cfg.Release.Name)
		if cfg.GatewayDomain != "" {
			spec["gateway"] = map[string]any{"domain": cfg.GatewayDomain}
		}
	}

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": spec,
		},
	}
	u.SetGroupVersionKind(h.Config.GVK)
	u.SetName(h.Config.CRName)
	u.SetAnnotations(map[string]string{
		annotations.ManagementStateAnnotation: string(managementState),
	})

	return u, nil
}

func sectionTitle(platformName common.Platform) string {
	if title, ok := sectionTitles[platformName]; ok {
		return title
	}
	return "MLflow"
}
