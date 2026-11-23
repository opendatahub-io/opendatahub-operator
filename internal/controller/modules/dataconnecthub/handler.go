package dataconnecthub

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	moduleName      = componentApi.DataConnectHubModuleName
	crName          = componentApi.DataConnectHubInstanceName
	deploymentName  = "dc-controller-manager"
	controllerImage = "RELATED_IMAGE_ODH_DATA_CONNECT_HUB_CONTROLLER_IMAGE"
)

var relatedImages = []string{
	"RELATED_IMAGE_ODH_DATA_CONNECT_HUB_REST_IMAGE",
	"RELATED_IMAGE_ODH_DATA_CONNECT_HUB_FLIGHT_IMAGE",
	"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
}

type handler struct {
	modules.BaseHandler
}

func NewHandler() *handler {
	return &handler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:            moduleName,
				CRName:          crName,
				ManifestDir:     "dataconnecthub",
				SourcePath:      "config/default",
				ControllerImage: controllerImage,
				RelatedImages:   relatedImages,
				DeploymentName:  deploymentName,
				GVK:             gvk.DataConnectHub,
			},
		},
	}
}

func (h *handler) PopulatePlatformModule(pm *configApi.PlatformModules, dscCtx *modules.DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSC == nil {
		return
	}
	state := dscCtx.DSC.Spec.Components.Data.DataConnectHub.ManagementState
	if state == "" {
		state = operatorv1.Removed
	}
	pm.DataConnectHub.ManagementState = state
}

func (h *handler) IsEnabled(pm *configApi.PlatformModules) bool {
	return pm != nil && pm.DataConnectHub.ManagementState == operatorv1.Managed
}

// BuildModuleCR constructs the independent DataConnectHub control CR.
func (h *handler) BuildModuleCR(
	ctx context.Context,
	cli client.Client,
	dscCtx *modules.DSCContext,
	cfg *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	if dscCtx == nil || dscCtx.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build DataConnectHub CR")
	}
	if cfg == nil {
		return nil, errors.New("module CR configuration is nil, cannot build DataConnectHub CR")
	}
	if cfg.ApplicationsNamespace == "" {
		return nil, errors.New("applications namespace is empty, cannot build DataConnectHub CR")
	}
	dataConnectHub := dscCtx.DSC.Spec.Components.Data.DataConnectHub
	platformGateway := map[string]any{
		"name":      gateway.GetDefaultGatewayName(),
		"namespace": gateway.GetGatewayNamespace(),
	}
	if dataConnectHub.Gateway != nil {
		platformGateway["name"] = dataConnectHub.Gateway.Name
		platformGateway["namespace"] = dataConnectHub.Gateway.Namespace
	}
	spec := map[string]any{
		"applicationsNamespace": cfg.ApplicationsNamespace,
		"distribution": map[string]any{
			"name":    distributionName(cfg.Release.Name),
			"version": cfg.Release.Version.String(),
		},
		"gateway": platformGateway,
	}

	if len(dataConnectHub.TokenReviewAudiences) > 0 {
		spec["tokenReviewAudiences"] = append([]string(nil), dataConnectHub.TokenReviewAudiences...)
	} else {
		// OpenShift publishes the service-account issuer used as the TokenReview
		// audience on clusters such as ROSA. Empty means use platform discovery.
		if cli == nil {
			return nil, errors.New("kubernetes client is nil, cannot discover DataConnectHub authentication settings")
		}
		issuer, err := cluster.GetClusterServiceAccountIssuer(ctx, cli)
		if err != nil {
			return nil, fmt.Errorf("discovering cluster service-account issuer: %w", err)
		}
		if issuer != "" {
			spec["tokenReviewAudiences"] = []string{issuer}
		}
	}

	u := &unstructured.Unstructured{Object: map[string]any{"spec": spec}}
	u.SetGroupVersionKind(h.Config.GVK)
	u.SetName(h.Config.CRName)
	return u, nil
}

func distributionName(platform common.Platform) string {
	switch platform {
	case cluster.OpenDataHub:
		return "OpenDataHub"
	case cluster.SelfManagedRhoai:
		return "SelfManagedRHOAI"
	case cluster.ManagedRhoai:
		return "ManagedRHOAI"
	case cluster.XKS:
		return "Standalone"
	default:
		return string(platform)
	}
}
