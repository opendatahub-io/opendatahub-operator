package mlflowoperator

import (
	"context"
	"errors"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	moduleName             = componentApi.MLflowOperatorComponentName
	crName                 = componentApi.MLflowOperatorInstanceName
	defaultGatewayName     = "data-science-gateway"
	readyConditionType     = componentApi.MLflowOperatorKind + status.ReadySuffix
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
	if platform == nil || platform.DSC == nil {
		return false
	}
	return platform.DSC.Spec.Components.MLflowOperator.ManagementState == operatorv1.Managed
}

func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform == nil || platform.DSC == nil {
		return nil, errors.New("platform context is nil, cannot build MLflowOperator CR")
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
		annotations.ManagementStateAnnotation: string(platform.DSC.Spec.Components.MLflowOperator.ManagementState),
	})

	return u, nil
}

func (h *handler) UpdateDSCComponentStatus(
	ctx context.Context,
	rr *types.ReconciliationRequest,
	platform *modules.PlatformContext,
) (metav1.ConditionStatus, error) {
	if platform == nil || platform.DSC == nil {
		return metav1.ConditionUnknown, nil
	}

	module := componentApi.MLflowOperator{}
	module.SetGroupVersionKind(gvk.MLflowOperator)
	module.SetName(crName)
	if err := rr.Client.Get(ctx, client.ObjectKeyFromObject(&module), &module); err != nil {
		if !k8serr.IsNotFound(err) {
			return metav1.ConditionUnknown, err
		}
	}

	dsc := platform.DSC
	ms := components.NormalizeManagementState(dsc.Spec.Components.MLflowOperator.ManagementState)
	dsc.Status.Components.MLflowOperator.ManagementState = ms
	dsc.Status.Components.MLflowOperator.MLflowOperatorCommonStatus = nil

	rr.Conditions.MarkFalse(readyConditionType)

	if !module.GetDeletionTimestamp().IsZero() {
		rr.Conditions.MarkFalse(
			readyConditionType,
			conditions.WithReason(status.DeletingReason),
			conditions.WithMessage(status.DeletingMessage),
		)
		return metav1.ConditionFalse, nil
	}

	if h.IsEnabled(platform) {
		dsc.Status.Components.MLflowOperator.MLflowOperatorCommonStatus = module.Status.MLflowOperatorCommonStatus.DeepCopy()
		if rc := conditions.FindStatusCondition(module.GetStatus(), status.ConditionTypeReady); rc != nil {
			rr.Conditions.MarkFrom(readyConditionType, *rc)
			return rc.Status, nil
		}

		rr.Conditions.MarkFalse(
			readyConditionType,
			conditions.WithReason(status.NotReadyReason),
			conditions.WithMessage("MLflowOperator module is not ready"),
		)
		return metav1.ConditionFalse, nil
	}

	rr.Conditions.MarkFalse(
		readyConditionType,
		conditions.WithReason(string(ms)),
		conditions.WithMessage("Component ManagementState is set to %s", string(ms)),
		conditions.WithSeverity(common.ConditionSeverityInfo),
	)
	return metav1.ConditionUnknown, nil
}

func sectionTitle(platformName common.Platform) string {
	if title, ok := sectionTitles[platformName]; ok {
		return title
	}
	return "MLflow"
}
