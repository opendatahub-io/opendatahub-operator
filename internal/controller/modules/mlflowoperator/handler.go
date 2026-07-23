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

func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil {
		return false
	}
	if platform.DSC != nil {
		return platform.DSC.Spec.Components.MLflowOperator.ManagementState == operatorv1.Managed
	}
	if platform.Platform != nil {
		return platform.Platform.Spec.Modules.MLflowOperator.ManagementState == operatorv1.Managed
	}
	return false
}

func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform == nil {
		return nil, errors.New("platform context is nil, cannot build MLflowOperator CR")
	}

	managementState, err := projectedManagementState(platform)
	if err != nil {
		return nil, err
	}

	// APPLICATIONS_NAMESPACE is injected directly into the operator Deployment so
	// the mlflow-operator process, cache, and namespaced RBAC all agree on one
	// startup-scoped target namespace.
	spec := map[string]any{
		"gatewayName":  defaultGatewayName,
		"sectionTitle": sectionTitle(platform.Release.Name),
	}
	if platform.GatewayDomain != "" {
		spec["gateway"] = map[string]any{"domain": platform.GatewayDomain}
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

func projectedManagementState(platform *modules.PlatformContext) (operatorv1.ManagementState, error) {
	if platform == nil {
		return "", errors.New("platform context is nil, cannot project MLflowOperator management state")
	}
	if platform.DSC != nil {
		return components.NormalizeManagementState(platform.DSC.Spec.Components.MLflowOperator.ManagementState), nil
	}
	if platform.Platform != nil {
		return normalizePlatformManagementState(platform.Platform), nil
	}
	return "", errors.New("neither DSC nor Platform CR exists, cannot build MLflowOperator CR")
}

func normalizePlatformManagementState(platform *configv1alpha1.Platform) operatorv1.ManagementState {
	if platform == nil {
		return operatorv1.Removed
	}
	return components.NormalizeManagementState(platform.Spec.Modules.MLflowOperator.ManagementState)
}
