package databaseservice

import (
	"context"
	"errors"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	moduleName = componentApi.DatabaseServiceComponentName
	crName     = componentApi.DatabaseServiceInstanceName
	chartDir   = "databaseservice"

	// Rendered Deployment name from the db-operator Helm chart
	// (config/chart/templates/apps_v1_deployment.yaml).
	deploymentName = "odh-db-operator-operator"

	// controllerImage is the RELATED_IMAGE_* env var holding the digest-pinned
	// module operator image. Add a matching imageOverrides entry once the
	// quay image digest is published for release builds.
	controllerImage = "RELATED_IMAGE_ODH_DB_OPERATOR_IMAGE"
)

type handler struct {
	modules.BaseHandler
}

func NewHandler() *handler {
	return &handler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:            moduleName,
				CRName:          crName,
				ChartDir:        chartDir,
				ReleaseName:     "opendatahub-db-operator",
				DeploymentName:  deploymentName,
				ControllerImage: controllerImage,
				GVK:             gvk.DatabaseService,
			},
		},
	}
}

func (h *handler) PopulatePlatformModule(pm *configApi.PlatformModules, dscCtx *modules.DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSC == nil {
		return
	}
	ms := dscCtx.DSC.Spec.Components.DatabaseService.ManagementState
	if ms == "" {
		ms = operatorv1.Removed
	}
	pm.DatabaseService.ManagementState = ms
}

func (h *handler) IsEnabled(modules *configApi.PlatformModules) bool {
	return modules != nil && modules.DatabaseService.ManagementState == operatorv1.Managed
}

func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	dscCtx *modules.DSCContext,
	_ *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	if dscCtx == nil || dscCtx.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build DatabaseService CR")
	}

	// DatabaseServiceSpec is empty on the module CRD; do not project managementState
	// or invent DSC-only fields such as defaultProvider.
	u := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": map[string]any{},
		},
	}
	u.SetGroupVersionKind(h.Config.GVK)
	u.SetName(h.Config.CRName)

	return u, nil
}
