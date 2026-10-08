package aihub

import (
	"context"
	"errors"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	crName = "default-aihub"
)

type handler struct {
	modules.BaseHandler
}

func NewHandler() *handler {
	return &handler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:            componentApi.AIHubModuleName,
				CRName:          crName,
				GVK:             gvk.AIHub,
				ManifestDir:     "aihub",
				SourcePath:      "overlays/aihub",
				DeploymentName:  "aihub-controller-manager",
				ControllerImage: "RELATED_IMAGE_ODH_MODEL_REGISTRY_OPERATOR_IMAGE",
				RelatedImages: []string{
					"RELATED_IMAGE_ODH_MODEL_REGISTRY_OPERATOR_IMAGE",
					"RELATED_IMAGE_ODH_MODEL_REGISTRY_IMAGE",
					"RELATED_IMAGE_POSTGRESQL_16_IMAGE",
					"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
					"RELATED_IMAGE_ODH_MODEL_METADATA_COLLECTION_IMAGE",
					"RELATED_IMAGE_ODH_MODEL_PERFORMANCE_DATA_IMAGE",
					"RELATED_IMAGE_ODH_MODEL_REGISTRY_JOB_ASYNC_UPLOAD_IMAGE",
				},
			},
		},
	}
}

func (h *handler) IsEnabled(modules *configApi.PlatformModules) bool {
	return modules != nil && modules.AIHub.ManagementState == operatorv1.Managed
}

func (h *handler) PopulatePlatformModule(pm *configApi.PlatformModules, dscCtx *modules.DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSC == nil {
		return
	}
	ms := dscCtx.DSC.Spec.Components.AIHub.ManagementState
	if ms == "" {
		ms = operatorv1.Removed
	}
	pm.AIHub.ManagementState = ms
}

func (h *handler) GetReadyConditionType() string {
	return "AIHub" + status.ReadySuffix
}

func (h *handler) WriteDSCComponentStatus(dsc *dscApi.DataScienceCluster, enabled bool, releases []common.ComponentRelease) {
	if dsc == nil {
		return
	}

	ms := operatorv1.Removed
	if enabled {
		ms = operatorv1.Managed
	}
	dsc.Status.Components.AIHub.ManagementState = ms

	instancesNamespace := ""
	if enabled {
		instancesNamespace = dsc.Spec.Components.AIHub.InstancesNamespace
	}

	componentStatus := &dsc.Status.Components.AIHub
	if componentStatus.AIHubCommonStatus == nil {
		if instancesNamespace == "" && len(releases) == 0 {
			return
		}
		componentStatus.AIHubCommonStatus = &componentApi.AIHubCommonStatus{}
	}
	componentStatus.InstancesNamespace = instancesNamespace
	componentStatus.Releases = releases
}

func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	dscCtx *modules.DSCContext,
	cfg *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	if dscCtx == nil || dscCtx.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build AIHub CR")
	}

	managementState := components.NormalizeManagementState(dscCtx.DSC.Spec.Components.AIHub.ManagementState)

	appNS := ""
	if cfg != nil {
		appNS = cfg.ApplicationsNamespace
	}

	instNS := dscCtx.DSC.Spec.Components.AIHub.InstancesNamespace
	if instNS == "" {
		instNS = appNS
	}

	spec := map[string]any{
		"applicationNamespace": appNS,
		"instancesNamespace":   instNS,
	}
	if cfg != nil && cfg.GatewayDomain != "" {
		spec["gateway"] = map[string]any{"domain": cfg.GatewayDomain}
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
