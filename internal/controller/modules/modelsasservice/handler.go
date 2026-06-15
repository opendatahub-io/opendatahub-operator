package modelsasservice

import (
	"context"
	"errors"
	"fmt"

	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	// ModuleName is the name of the ModelsAsService module.
	ModuleName = componentApi.ModelsAsServiceComponentName
	moduleName = ModuleName
	// CRName is the default name of the ModelsAsService CR.
	CRName = componentApi.ModelsAsServiceInstanceName
	crName = CRName
)

// TenantSubscriptionNamespace is where Tenant CRs are created by maas-controller.
// This must match the --maas-subscription-namespace flag in maas-controller (default: models-as-a-service).
// Matches MaaSSubscriptionNamespace in the component handler.
const TenantSubscriptionNamespace = "models-as-a-service"

type handler struct {
	modules.BaseHandler
}

func NewHandler() *handler {
	return &handler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:   moduleName,
				CRName: crName,
				// MaaS uses Kustomize for manifests (not Helm charts)
				ManifestDir: "maas",
				SourcePath:  "overlays/odh",
				GVK:         gvk.ModelsAsService,
				RelatedImages: []string{
					"RELATED_IMAGE_ODH_MAAS_CONTROLLER_IMAGE",
					"RELATED_IMAGE_ODH_MAAS_API_IMAGE",
					"RELATED_IMAGE_ODH_AI_GATEWAY_PAYLOAD_PROCESSING_IMAGE",
					"RELATED_IMAGE_UBI_MINIMAL_IMAGE",
				},
				// ControllerImage declares which RELATED_IMAGE_* env var contains the
				// maas-controller image digest override. The module framework injects
				// this into the Deployment's manager container.
				ControllerImage: "RELATED_IMAGE_ODH_MAAS_CONTROLLER_IMAGE",
			},
		},
	}
}

// IsEnabled checks whether the ModelsAsService module should be deployed.
// MaaS is a KServe sub-component, so it requires:
// 1. KServe.ManagementState == Managed
// 2. KServe.ModelsAsService.ManagementState == Managed
//
// In DSC mode (DSCI present), reads DSC.Spec.Components.Kserve.
// In Platform mode (xKS), returns true (platform-managed).
func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil {
		return false
	}

	// Platform mode (xKS): MaaS enabled by platform config
	if platform.Platform != nil {
		return true
	}

	// DSC mode: check KServe and ModelsAsService management states
	if platform.DSC != nil {
		if platform.DSC.Spec.Components.Kserve.ManagementState != operatorv1.Managed {
			return false
		}
		return platform.DSC.Spec.Components.Kserve.ModelsAsService.ManagementState == operatorv1.Managed
	}

	return false
}

// BuildModuleCR creates the ModelsAsService module CR with an empty spec.
// This matches the component handler's NewCRObject behavior, which returns
// ModelsAsServiceSpec{} (empty struct). The CR serves as an anchor for the
// module lifecycle; actual configuration is managed by maas-controller.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform == nil {
		return nil, errors.New("platform context is nil, cannot build ModelsAsService CR")
	}

	// Both DSC and Platform modes: return empty spec to match component NewCRObject behavior.
	// The component handler's NewCRObject returns ModelsAsServiceSpec{} (empty struct).
	// The module framework always populates either platform.DSC or platform.Platform, so we
	// don't validate which one is set - empty spec is correct for both.
	spec := map[string]any{}

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": spec,
		},
	}
	u.SetGroupVersionKind(h.Config.GVK)
	u.SetName(h.Config.CRName)

	return u, nil
}

// GetModuleStatus reads the Tenant CR status and converts it to ModuleStatus.
// This overrides the base implementation because maas-controller sets status
// conditions on the Tenant CR, not the ModelsAsService CR. The Tenant CR is
// the source of truth for MaaS operational health.
//
// The ModelsAsService CR exists as the module anchor for ODH operator lifecycle,
// but maas-controller does not reconcile it or set its status. Instead,
// maas-controller reconciles Tenant CRs which manage maas-api deployments.
func (h *handler) GetModuleStatus(ctx context.Context, cli client.Client) (*modules.ModuleStatus, error) {
	// Read Tenant CR from the MaaS subscription namespace
	tenant := &maasv1alpha1.Tenant{}
	tenant.Name = maasv1alpha1.TenantInstanceName
	tenant.Namespace = TenantSubscriptionNamespace

	if err := cli.Get(ctx, client.ObjectKeyFromObject(tenant), tenant); err != nil {
		if k8serr.IsNotFound(err) {
			// Tenant CR doesn't exist yet - module is being provisioned
			return &modules.ModuleStatus{
				Conditions: []metav1.Condition{
					{
						Type:    status.ConditionTypeReady,
						Status:  metav1.ConditionFalse,
						Reason:  status.NotReadyReason,
						Message: "Tenant CR not created yet",
					},
				},
				ObservedGeneration: 0,
				Generation:         0,
			}, nil
		}
		if apimeta.IsNoMatchError(err) {
			// Tenant CRD not installed - maas-controller not deployed yet
			return &modules.ModuleStatus{
				Conditions: []metav1.Condition{
					{
						Type:    status.ConditionTypeReady,
						Status:  metav1.ConditionFalse,
						Reason:  status.NotReadyReason,
						Message: "Tenant CRD not installed",
					},
				},
				ObservedGeneration: 0,
				Generation:         0,
			}, nil
		}
		return nil, fmt.Errorf("failed to get Tenant %s/%s: %w", tenant.Namespace, tenant.Name, err)
	}

	// Check if Tenant is being deleted
	if !tenant.DeletionTimestamp.IsZero() {
		return &modules.ModuleStatus{
			Conditions: []metav1.Condition{
				{
					Type:    status.ConditionTypeReady,
					Status:  metav1.ConditionFalse,
					Reason:  status.DeletingReason,
					Message: status.DeletingMessage,
				},
			},
			ObservedGeneration: 0,
			Generation:         tenant.Generation,
		}, nil
	}

	// Return Tenant status conditions
	// The maas-controller sets Ready, Degraded, and other conditions on Tenant
	// Note: Tenant doesn't have ObservedGeneration in status, so we use 0
	return &modules.ModuleStatus{
		Conditions:         tenant.Status.Conditions,
		ObservedGeneration: 0, // Tenant status doesn't track ObservedGeneration
		Generation:         tenant.Generation,
	}, nil
}
