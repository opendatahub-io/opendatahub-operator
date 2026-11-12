package synthetice2e

import (
	"context"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
)

const (
	ModuleName      = "synthetice2e"
	CRName          = "default-synthetic"
	DeploymentName  = "synthetic-module-operator"
	ControllerImage = "RELATED_IMAGE_ODH_SYNTHETIC_MODULE_OPERATOR_IMAGE"

	FaultAnnotationKey            = "test.synthetic.platform.opendatahub.io/fault"
	FaultModeReadyFalse           = "ready-false"
	FaultModeDegraded             = "degraded"
	FaultModeStaleGeneration      = "stale-generation"
	FaultModeStalePlatformRelease = "stale-platform-release"
	FaultModeHoldFinalizer        = "hold-finalizer"
)

var moduleGVK = schema.GroupVersionKind{
	Group:   "synthetic.platform.opendatahub.io",
	Version: "v1alpha1",
	Kind:    "SyntheticModule",
}

type handler struct {
	modules.BaseHandler
}

func NewHandler() *handler {
	return &handler{BaseHandler: modules.BaseHandler{Config: modules.ModuleConfig{
		Name:              ModuleName,
		CRName:            CRName,
		ReleaseName:       DeploymentName,
		ChartDir:          ModuleName,
		NamespaceValueKey: "namespace",
		DeploymentName:    DeploymentName,
		GVK:               moduleGVK,
		ControllerImage:   ControllerImage,
	}}}
}

func (h *handler) IsEnabled(platformModules *configApi.PlatformModules) bool {
	return platformModules != nil && platformModules.SyntheticE2E.ManagementState == operatorv1.Managed
}

func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	_ *modules.DSCContext,
	_ *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"message": "synthetic module ready",
		},
	}}
	u.SetGroupVersionKind(h.Config.GVK)
	u.SetName(h.Config.CRName)

	return u, nil
}
