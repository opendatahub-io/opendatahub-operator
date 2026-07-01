package aigateway

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
)

const (
	moduleName = componentApi.AIGatewayComponentName
	crName     = componentApi.AIGatewayInstanceName

	// ReadyConditionType is the DSC condition type set by the component handler.
	// Follows the <Kind>Ready pattern used by all other ODH components.
	ReadyConditionType = componentApi.AIGatewayKind + status.ReadySuffix // "AIGatewayReady"

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
	// the sub-module (batch-gateway-operator, maas-controller) deployments it creates;
	// injected as RELATED_IMAGE_* env vars on the ai-gateway-operator container.
	relatedImages = []string{
		// Batch Gateway images
		"RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_OPERATOR_IMAGE",
		"RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_APISERVER_IMAGE",
		"RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_PROCESSOR_IMAGE",
		"RELATED_IMAGE_ODH_LLM_D_BATCH_GATEWAY_GC_IMAGE",
		// MaaS images (commented out until onboarded to build configs - RHOAIENG-66857)
		// "RELATED_IMAGE_ODH_MAAS_CONTROLLER_IMAGE",
		// "RELATED_IMAGE_ODH_MAAS_API_IMAGE",
		// "RELATED_IMAGE_ODH_AI_GATEWAY_PAYLOAD_PROCESSING_IMAGE",
		// "RELATED_IMAGE_UBI_MINIMAL_IMAGE",
	}
)

// handler implements ModuleHandler for AIGateway.
type handler struct {
	modules.BaseHandler
}

// componentHandler implements ComponentHandler for AIGateway status reporting.
// It wraps the module handler to provide component registry methods.
type componentHandler struct {
	*handler
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

// NewComponentHandler creates a component handler wrapper for status reporting.
func NewComponentHandler() *componentHandler {
	return &componentHandler{handler: NewHandler()}
}

func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil {
		return false
	}
	if platform.DSC != nil {
		aigateway := platform.DSC.Spec.Components.AIGateway

		// Top-level AIGateway ManagementState acts as master switch
		if aigateway.ManagementState == operatorv1.Removed {
			return false
		}

		// AIGateway is enabled if either sub-component is Managed
		return aigateway.ModelsAsService.ManagementState == operatorv1.Managed ||
			aigateway.BatchGateway.ManagementState == operatorv1.Managed
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

// ComponentHandler interface implementation for componentHandler
// These methods allow AIGateway to be registered as a Component for status reporting

func (c *componentHandler) Init(platform common.Platform, cfg operatorconfig.OperatorSettings) error {
	// No additional initialization needed - module handler already initialized
	return nil
}

func (c *componentHandler) GetName() string {
	return moduleName
}

func (c *componentHandler) IsEnabled(dsc *dscv2.DataScienceCluster) bool {
	if dsc == nil {
		return false
	}

	aigateway := dsc.Spec.Components.AIGateway

	// Top-level AIGateway ManagementState acts as master switch
	if aigateway.ManagementState == operatorv1.Removed {
		return false
	}

	// AIGateway is enabled if either sub-component is Managed
	return aigateway.ModelsAsService.ManagementState == operatorv1.Managed ||
		aigateway.BatchGateway.ManagementState == operatorv1.Managed
}

func (c *componentHandler) NewCRObject(ctx context.Context, cli client.Client, dsc *dscv2.DataScienceCluster) (common.PlatformObject, error) {
	// AIGateway CR is managed externally by the module reconciler, not by the component reconciler
	// Return nil to indicate no component-owned CR (similar to how some components work)
	return nil, nil
}

func (c *componentHandler) NewComponentReconciler(ctx context.Context, mgr ctrl.Manager) error {
	// Module reconciler is already set up separately via NewModuleReconciler
	// No additional component reconciler needed
	return nil
}

func (c *componentHandler) UpdateDSCStatus(ctx context.Context, rr *odhtypes.ReconciliationRequest) (metav1.ConditionStatus, error) {
	// Set AIGatewayReady=False as the safe default until we confirm the CR is ready.
	// This mirrors the pattern used by all other ODH components (e.g. KServe, Dashboard).
	rr.Conditions.MarkFalse(ReadyConditionType)

	// Get the AIGateway CR managed by the ai-gateway-operator.
	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(c.Config.GVK)
	cr.SetName(componentApi.AIGatewayInstanceName)

	if err := rr.Client.Get(ctx, client.ObjectKeyFromObject(cr), cr); err != nil {
		if k8serr.IsNotFound(err) {
			return metav1.ConditionFalse, nil
		}
		return metav1.ConditionUnknown, err
	}

	// Mirror managementState into DSC status.components.aigateway.
	dsc, ok := rr.Instance.(*dscv2.DataScienceCluster)
	if !ok {
		return metav1.ConditionUnknown, errors.New("instance is not a DataScienceCluster")
	}
	dsc.Status.Components.AIGateway.ManagementState = dsc.Spec.Components.AIGateway.ManagementState

	// Mirror the AIGateway CR's Ready condition onto the DSC as AIGatewayReady.
	conditions, found, err := unstructured.NestedSlice(cr.Object, "status", "conditions")
	if err != nil {
		return metav1.ConditionFalse, err
	}
	if !found {
		return metav1.ConditionFalse, nil
	}

	for _, cond := range conditions {
		condMap, ok := cond.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _, _ := unstructured.NestedString(condMap, "type")
		condStatus, _, _ := unstructured.NestedString(condMap, "status")
		condReason, _, _ := unstructured.NestedString(condMap, "reason")
		condMsg, _, _ := unstructured.NestedString(condMap, "message")

		if condType == status.ConditionTypeReady {
			rc := common.Condition{
				Type:    condType,
				Status:  metav1.ConditionStatus(condStatus),
				Reason:  condReason,
				Message: condMsg,
			}
			rr.Conditions.MarkFrom(ReadyConditionType, rc)
			if condStatus == string(metav1.ConditionTrue) {
				return metav1.ConditionTrue, nil
			}
			return metav1.ConditionFalse, nil
		}
	}

	return metav1.ConditionFalse, nil
}
