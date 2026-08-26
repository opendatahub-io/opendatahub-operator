package modelregistry

import (
	"context"
	"errors"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	moduleName = componentApi.ModelRegistryComponentName
	crName     = "default-aihub"
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
				GVK:             gvk.AIHub,
				ManifestDir:     "modelregistry",
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

func (h *handler) IsEnabled(modules *configv1alpha1.PlatformModules) bool {
	return modules != nil && modules.ModelRegistry.ManagementState == operatorv1.Managed
}

func (h *handler) PopulatePlatformModule(pm *configv1alpha1.PlatformModules, dscCtx *modules.DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSC == nil {
		return
	}
	ms := dscCtx.DSC.Spec.Components.ModelRegistry.ManagementState
	if ms == "" {
		ms = operatorv1.Removed
	}
	pm.ModelRegistry.ManagementState = ms
}

func (h *handler) GetReadyConditionType() string {
	return componentApi.ModelRegistryKind + status.ReadySuffix
}

func (h *handler) WriteDSCComponentStatus(dsc *dscv2.DataScienceCluster, enabled bool, releases []common.ComponentRelease) {
	if dsc == nil {
		return
	}

	ms := operatorv1.Removed
	if enabled {
		ms = operatorv1.Managed
	}
	dsc.Status.Components.ModelRegistry.ManagementState = ms

	if len(releases) > 0 {
		if dsc.Status.Components.ModelRegistry.ModelRegistryCommonStatus == nil {
			dsc.Status.Components.ModelRegistry.ModelRegistryCommonStatus = &componentApi.ModelRegistryCommonStatus{}
		}
		dsc.Status.Components.ModelRegistry.Releases = releases
	} else if dsc.Status.Components.ModelRegistry.ModelRegistryCommonStatus != nil {
		dsc.Status.Components.ModelRegistry.Releases = nil
	}
}

// WriteLegacyStatusFields mirrors registriesNamespace from the DSC spec into
// dsc.status.components.modelRegistry so consumers (e.g. odh-dashboard) can
// resolve the model registries namespace from DSC status.
//
// TODO: Remove once consumers read registriesNamespace directly from the AIHub
// module CR instead of DSC status.
func (h *handler) WriteLegacyStatusFields(
	_ context.Context,
	_ client.Client,
	dsc *dscv2.DataScienceCluster,
	enabled bool,
) error {
	if dsc == nil {
		return nil
	}

	if !enabled {
		writeDSCRegistriesNamespace(dsc, false, "")
		return nil
	}

	writeDSCRegistriesNamespace(dsc, true, dsc.Spec.Components.ModelRegistry.RegistriesNamespace)
	return nil
}

func writeDSCRegistriesNamespace(dsc *dscv2.DataScienceCluster, enabled bool, registriesNamespace string) {
	if dsc == nil {
		return
	}

	if !enabled || registriesNamespace == "" {
		if dsc.Status.Components.ModelRegistry.ModelRegistryCommonStatus != nil {
			dsc.Status.Components.ModelRegistry.RegistriesNamespace = ""
		}
		return
	}

	if dsc.Status.Components.ModelRegistry.ModelRegistryCommonStatus == nil {
		dsc.Status.Components.ModelRegistry.ModelRegistryCommonStatus = &componentApi.ModelRegistryCommonStatus{}
	}

	dsc.Status.Components.ModelRegistry.RegistriesNamespace = registriesNamespace
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

	managementState := components.NormalizeManagementState(dscCtx.DSC.Spec.Components.ModelRegistry.ManagementState)

	appNS := ""
	if cfg != nil {
		appNS = cfg.ApplicationsNamespace
	}

	instNS := dscCtx.DSC.Spec.Components.ModelRegistry.RegistriesNamespace
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
