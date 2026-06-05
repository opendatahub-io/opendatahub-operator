package batchgateway

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
	moduleName = componentApi.BatchGatewayComponentName
	crName     = componentApi.BatchGatewayInstanceName

	manifestDir = "batchgateway"

	// paramsPath is the Kustomize base under manifestDir holding the shared
	// params.env that both overlays (odh, rhoai) reference.
	paramsPath = "base"
)

var (
	sourcePathByPlatform = map[common.Platform]string{
		cluster.OpenDataHub:      "overlays/odh",
		cluster.SelfManagedRhoai: "overlays/rhoai",
	}

	imageParamMap = map[string]string{
		"LLM_D_BATCH_GATEWAY_OPERATOR_IMAGE":  "RELATED_IMAGE_ODH_BATCH_GATEWAY_OPERATOR_IMAGE",
		"LLM_D_BATCH_GATEWAY_APISEVER_IMAGE":  "RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_APISERVER_IMAGE",
		"LLM_D_BATCH_GATEWAY_PROCESSOR_IMAGE": "RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_PROCESSOR_IMAGE",
		"LLM_D_BATCH_GATEWAY_GC_IMAGE":        "RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_GC_IMAGE",
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
				ImageParamMap:        imageParamMap,
				ParamsPath:           paramsPath,
				GVK:                  gvk.BatchGateway,
			},
		},
	}
}

func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil {
		return false
	}
	if platform.DSC != nil {
		return platform.DSC.Spec.Components.BatchGateway.ManagementState == operatorv1.Managed
	}
	return false
}

// BuildModuleCR projects the DSC BatchGateway configuration onto the
// batchgateways.components.platform.opendatahub.io CR. Required spec fields not
// surfaced in BatchGatewayCommonSpec are expected to be defaulted by the
// batchgateway operator.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform == nil {
		return nil, errors.New("platform context is nil, cannot build BatchGateway CR")
	}
	if platform.DSC == nil {
		return nil, errors.New("DSC is not available, cannot build BatchGateway CR")
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(
		&platform.DSC.Spec.Components.BatchGateway.BatchGatewayCommonSpec,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to convert BatchGatewayCommonSpec to unstructured: %w", err)
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
