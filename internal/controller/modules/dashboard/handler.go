package dashboard

import (
	"context"
	"errors"
	"fmt"

	helmtypes "github.com/k8s-manifest-kit/engine/pkg/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	moduleName = componentApi.DashboardComponentName
	// crName must match dashboard-operator CRD CEL (default-dashboard); see odh-dashboard#8093.
	crName = componentApi.DashboardInstanceName

	// defaultControllerImageTag is used when RELATED_IMAGE_ODH_DASHBOARD_OPERATOR_IMAGE
	// is unset on the operator pod. The RHOAI chart defaults to :latest, which does not
	// exist; :main is the published tag until Build-Config supplies a digest override.
	defaultControllerImageTag = "main"

	errCertManagerCRDsRequired = "cert-manager CRDs (Certificate, CertificateRequest, Issuer) are required for dashboard webhook TLS"
)

type handler struct {
	modules.BaseHandler
}

func NewHandler() *handler {
	return &handler{
		BaseHandler: modules.BaseHandler{
			Config: modules.ModuleConfig{
				Name:              moduleName,
				CRName:            crName,
				ReleaseName:       "dashboard-operator",
				ChartDir:          "dashboard-operator",
				NamespaceValueKey: "namespace",
				Values: map[string]any{
					// Chart defaults namePrefix to "odh-", producing Deployment
					// "odh-dashboard-operator". Clear it so the Deployment name matches
					// ReleaseName for module env injection (deploymentNameFromManifests).
					"namePrefix": "",
					// RHOAI chart defaults image.tag to latest (not published). injectModuleEnv
					// overrides the full image when RELATED_IMAGE_* is set on the operator pod.
					"image": map[string]any{
						"tag": defaultControllerImageTag,
					},
					// Preserve chart-aligned webhook defaults; cert-manager gating overrides
					// enabled/certManager.enabled in GetOperatorManifests.
					"webhook": map[string]any{
						"port": 9443,
					},
				},
				GVK:             gvk.Dashboard, // components.platform.opendatahub.io/v1alpha1/Dashboard
				ControllerImage: "RELATED_IMAGE_ODH_DASHBOARD_OPERATOR_IMAGE",
				RelatedImages:   relatedImages(),
			},
		},
	}
}

// ValidatePrerequisites ensures cert-manager CRDs are present before provisioning
// dashboard-operator with webhook TLS via cert-manager.
func (h *handler) ValidatePrerequisites(platform *modules.PlatformContext) error {
	if platform == nil || !platform.CertManagerCRDsAvailable {
		return errors.New(errCertManagerCRDsRequired)
	}
	return nil
}

// GetOperatorManifests renders the dashboard-operator chart and gates webhook TLS
// on cert-manager CRD availability.
func (h *handler) GetOperatorManifests(platform *modules.PlatformContext) modules.OperatorManifests {
	manifests := h.BaseHandler.GetOperatorManifests(platform)
	if platform == nil || len(manifests.HelmCharts) != 1 {
		return manifests
	}

	origValues := manifests.HelmCharts[0].Values
	certManagerAvailable := platform.CertManagerCRDsAvailable
	manifests.HelmCharts[0].Values = func(ctx context.Context) (helmtypes.Values, error) {
		vals := make(helmtypes.Values)
		if origValues != nil {
			v, err := origValues(ctx)
			if err != nil {
				return nil, err
			}
			if v != nil {
				vals = v
			}
		}

		var existingWebhook map[string]any
		if w, ok := vals["webhook"].(map[string]any); ok {
			existingWebhook = w
		}
		vals["webhook"] = mergeWebhookHelmValues(existingWebhook, certManagerAvailable)
		return vals, nil
	}

	return manifests
}

func mergeWebhookHelmValues(existing map[string]any, certManagerAvailable bool) map[string]any {
	merged := webhookHelmValues(certManagerAvailable)
	for k, v := range existing {
		if _, set := merged[k]; !set {
			merged[k] = v
		}
	}

	existingCM, existingOk := existing["certManager"].(map[string]any)
	mergedCM, mergedOk := merged["certManager"].(map[string]any)
	if existingOk && mergedOk {
		for k, v := range existingCM {
			if _, set := mergedCM[k]; !set {
				mergedCM[k] = v
			}
		}
	}

	return merged
}

func webhookHelmValues(certManagerAvailable bool) map[string]any {
	return map[string]any{
		"enabled": certManagerAvailable,
		"certManager": map[string]any{
			"enabled": certManagerAvailable,
		},
	}
}

// IsEnabled checks whether the dashboard module should be deployed based on
// DSC.Spec.Components.Dashboard.ManagementState.
func (h *handler) IsEnabled(platform *modules.PlatformContext) bool {
	if platform == nil || platform.DSC == nil {
		return false
	}
	return platform.DSC.Spec.Components.Dashboard.ManagementState == operatorv1.Managed
}

// BuildModuleCR projects user-facing DSC dashboard configuration and platform
// fields from PlatformContext onto the module CR.
func (h *handler) BuildModuleCR(
	_ context.Context,
	_ client.Client,
	platform *modules.PlatformContext,
) (*unstructured.Unstructured, error) {
	if platform == nil {
		return nil, errors.New("platform context is nil, cannot build dashboard CR")
	}
	if platform.DSC == nil {
		return nil, errors.New("DSC is nil, cannot build dashboard CR")
	}

	spec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&platform.DSC.Spec.Components.Dashboard)
	if err != nil {
		return nil, fmt.Errorf("failed to convert DSCDashboard to unstructured: %w", err)
	}

	if platform.GatewayDomain != "" {
		spec["gateway"] = map[string]any{
			"domain": platform.GatewayDomain,
		}
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
