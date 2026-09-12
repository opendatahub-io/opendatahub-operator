package aipipelines

import (
	"context"
	"errors"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	moduleName          = componentApi.AIPipelinesComponentName
	crName              = "default-aipipelines"
	deploymentName      = "data-science-pipelines-operator-controller-manager"
	moduleControllerEnv = "DSPO_ENABLEAIPIPELINESMODULECONTROLLER"
	platformVersionEnv  = "DSPO_PLATFORMVERSION"
	controllerImageEnv  = "RELATED_IMAGE_ODH_DATA_SCIENCE_PIPELINES_OPERATOR_CONTROLLER_IMAGE"
	odhOverlayPath      = "overlays/odh/dspo"
	rhoaiOverlayPath    = "overlays/rhoai/dspo"
)

var overlayByPlatform = map[common.Platform]string{
	cluster.ManagedRhoai:     rhoaiOverlayPath,
	cluster.SelfManagedRhoai: rhoaiOverlayPath,
	cluster.OpenDataHub:      odhOverlayPath,
}

var relatedImages = []string{
	"RELATED_IMAGE_ODH_ML_PIPELINES_API_SERVER_V2_IMAGE",
	"RELATED_IMAGE_ODH_ML_PIPELINES_PERSISTENCEAGENT_V2_IMAGE",
	"RELATED_IMAGE_ODH_ML_PIPELINES_SCHEDULEDWORKFLOW_V2_IMAGE",
	"RELATED_IMAGE_ODH_DATA_SCIENCE_PIPELINES_ARGO_ARGOEXEC_IMAGE",
	"RELATED_IMAGE_ODH_DATA_SCIENCE_PIPELINES_ARGO_WORKFLOWCONTROLLER_IMAGE",
	"RELATED_IMAGE_ODH_ML_PIPELINES_DRIVER_IMAGE",
	"RELATED_IMAGE_ODH_ML_PIPELINES_LAUNCHER_IMAGE",
	"RELATED_IMAGE_ODH_MLMD_GRPC_SERVER_IMAGE",
	"RELATED_IMAGE_ODH_ML_PIPELINES_RUNTIME_GENERIC_IMAGE",
	"RELATED_IMAGE_DSP_PROXYV2_IMAGE",
	"RELATED_IMAGE_DSP_MARIADB_IMAGE",
	"RELATED_IMAGE_DSP_TOOLBOX_IMAGE",
	"RELATED_IMAGE_DSP_INSTRUCTLAB_NVIDIA_IMAGE",
	"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
	"RELATED_IMAGE_ODH_PIPELINES_COMPONENTS_IMAGE",
	"RELATED_IMAGE_ODH_AUTOML_IMAGE",
	"RELATED_IMAGE_ODH_AUTORAG_IMAGE",
}

type handler struct {
	modules.BaseHandler
}

func NewHandler() *handler {
	return &handler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:                 moduleName,
				CRName:               crName,
				GVK:                  gvk.AIPipelines,
				ManifestDir:          componentApi.DataSciencePipelinesComponentName,
				SourcePath:           odhOverlayPath,
				SourcePathByPlatform: overlayByPlatform,
				DeploymentName:       deploymentName,
				ControllerImage:      controllerImageEnv,
				RelatedImages:        relatedImages,
				ExtraEnv: map[string]string{
					moduleControllerEnv: "true",
				},
			},
		},
	}
}

func (h *handler) PopulatePlatformModule(pm *configv1alpha1.PlatformModules, dscCtx *modules.DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSC == nil {
		return
	}

	managementState := dscCtx.DSC.Spec.Components.AIPipelines.ManagementState
	if managementState == "" {
		managementState = operatorv1.Removed
	}
	pm.AIPipelines.ManagementState = managementState
}

func (h *handler) IsEnabled(platformModules *configv1alpha1.PlatformModules) bool {
	return platformModules != nil && platformModules.AIPipelines.ManagementState == operatorv1.Managed
}

func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	dscCtx *modules.DSCContext,
	_ *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	if dscCtx == nil || dscCtx.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build AIPipelines CR")
	}

	argoState := operatorv1.Managed
	if argo := dscCtx.DSC.Spec.Components.AIPipelines.ArgoWorkflowsControllers; argo != nil && argo.ManagementState != "" {
		argoState = components.NormalizeManagementState(argo.ManagementState)
	}

	u := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"argoWorkflowsControllers": map[string]any{
				"managementState": string(argoState),
			},
		},
	}}
	u.SetGroupVersionKind(h.Config.GVK)
	u.SetName(h.Config.CRName)

	return u, nil
}

func (h *handler) GetPlatformEnv(platform *modules.PlatformContext) map[string]string {
	if platform == nil {
		return nil
	}

	return map[string]string{
		platformVersionEnv: platform.Release.Version.String(),
	}
}

// CleanupLegacyCR removes the in-tree DataSciencePipelines CR after its
// replacement AIPipelines CR has reached Ready. When AIPipelines is Removed,
// no handoff is needed and the legacy CR can be removed immediately.
func (h *handler) CleanupLegacyCR(ctx context.Context, cli client.Client, dsc *dscv2.DataScienceCluster) error {
	if cli == nil || dsc == nil {
		return nil
	}

	legacy := &componentApi.DataSciencePipelines{}
	legacy.SetName(componentApi.DataSciencePipelinesInstanceName)
	if err := cli.Get(ctx, client.ObjectKeyFromObject(legacy), legacy); err != nil {
		if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
			return nil
		}
		return err
	}

	if !ownedByDSC(legacy, dsc) {
		return nil
	}

	if components.NormalizeManagementState(dsc.Spec.Components.AIPipelines.ManagementState) == operatorv1.Managed {
		moduleStatus, err := h.GetModuleStatus(ctx, cli)
		if err != nil {
			if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
				return nil
			}
			return err
		}
		if moduleStatus.ObservedGeneration != moduleStatus.Generation || !moduleReady(moduleStatus) {
			return nil
		}
	}

	if err := cli.Delete(ctx, legacy); err != nil && !k8serr.IsNotFound(err) {
		return err
	}
	logf.FromContext(ctx).Info("deleted legacy DataSciencePipelines CR after AIPipelines module handoff",
		"name", legacy.GetName())
	return nil
}

func moduleReady(moduleStatus *modules.ModuleStatus) bool {
	if moduleStatus == nil {
		return false
	}
	for _, condition := range moduleStatus.Conditions {
		if condition.Type == status.ConditionTypeReady {
			return condition.Status == metav1.ConditionTrue
		}
	}
	return false
}

func ownedByDSC(obj client.Object, dsc *dscv2.DataScienceCluster) bool {
	for _, owner := range obj.GetOwnerReferences() {
		if owner.UID == dsc.GetUID() {
			return true
		}
	}
	return false
}

// WriteDSCComponentStatus is declared explicitly to document that the AIPipelines
// module owns the v2 DSC status stanza, including release mirroring.
func (h *handler) WriteDSCComponentStatus(
	dsc *dscv2.DataScienceCluster,
	enabled bool,
	releases []common.ComponentRelease,
) {
	h.BaseHandler.WriteDSCComponentStatus(dsc, enabled, releases)
}
