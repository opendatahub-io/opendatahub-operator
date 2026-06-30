package aigateway

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
	moduleName = componentApi.AIGatewayComponentName
	crName     = componentApi.AIGatewayInstanceName

	// manifestDir is the directory (relative to ManifestsBasePath, e.g.
	// /opt/manifests/aigateway) where get_all_manifests.sh places the
	// ai-gateway-operator repo's get the full config which has its own manifests + sub-modulars'.
	manifestDir = "aigateway"

	// when module name does not match its deployment name, need explicitly set it for env injection work.
	deploymentName = "ai-gateway-operator"

	// initContainerName is the init container in the ai-gateway-operator
	// Deployment that shares the operator image; its image is overridden with
	// controllerImage at inject time alongside the manager container.
	initContainerName = "copy-manifests"

	// controllerImage is the RELATED_IMAGE_* env var holding the digest-pinned
	// ai-gateway-operator image. The inject action overwrites the operator
	// container's image and initContaier if named copy-manifests with this value at deploy time.
	controllerImage = "RELATED_IMAGE_ODH_AI_GATEWAY_OPERATOR_IMAGE"
)

var (
	// sourcePathByPlatform selects the Kustomize overlay shipped in the
	// ai-gateway-operator config per platform flavor.
	sourcePathByPlatform = map[common.Platform]string{
		cluster.OpenDataHub:      "overlays/odh",
		cluster.SelfManagedRhoai: "overlays/rhoai",
	}

	// relatedImages are the operand images the ai-gateway-operator passes to
	// the sub-module (batch-gateway-operator) deployments it creates; injected as
	// RELATED_IMAGE_* env vars on the ai-g ateway-operator container.
	// TODO: append more for maas images.
	relatedImages = []string{
		"RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_OPERATOR_IMAGE",
		"RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_APISERVER_IMAGE",
		"RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_PROCESSOR_IMAGE",
		"RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_GC_IMAGE",
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
				ContextDir:           "manifests/ai-gateway-operator",
				SourcePathByPlatform: sourcePathByPlatform,
				ControllerImage:      controllerImage,
				InitContainerName:    initContainerName, // use same controller image for initContainer
				RelatedImages:        relatedImages,
				DeploymentName:       deploymentName, // different name need to set explicltiy
				GVK:                  gvk.AIGateway,
			},
		},
	}
}

func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil {
		return false
	}
	if platform.DSC != nil {
		// AIGateway is enabled if either sub-component is Managed
		return platform.DSC.Spec.Components.AIGateway.ModelsAsService.ManagementState == operatorv1.Managed ||
			platform.DSC.Spec.Components.AIGateway.BatchGateway.ManagementState == operatorv1.Managed
	}
	return false
}

// BuildModuleCR projects the DSC AIGateway configuration onto the
// aigateways.components.platform.opendatahub.io CR. The DSC-level
// managementState is intentionally excluded; only AIGatewayCommonSpec is
// projected into the module CR.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform == nil {
		return nil, errors.New("platform context is nil, cannot build AIGateway CR")
	}
	if platform.DSC == nil {
		return nil, errors.New("DSC is not available, cannot build AIGateway CR")
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(
		&platform.DSC.Spec.Components.AIGateway.AIGatewayCommonSpec,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to convert AIGatewayCommonSpec to unstructured: %w", err)
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
