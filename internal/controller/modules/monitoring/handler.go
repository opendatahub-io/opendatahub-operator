package monitoring

import (
	"context"
	"errors"
	"fmt"

	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"
	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	moduleName                 = serviceApi.MonitoringServiceName
	crName                     = serviceApi.MonitoringInstanceName
	monitoringNamespaceHelmKey = "monitoringNamespace"
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
				ReleaseName:       "odh-observability",
				ChartDir:          "odh-observability",
				NamespaceValueKey: "operatorNamespace",
				GVK:               gvk.Monitoring,
				DeploymentName:    "odh-observability",
				ControllerImage:   "RELATED_IMAGE_ODH_OBSERVABILITY_IMAGE",
				RelatedImages: []string{
					"RELATED_IMAGE_ODH_KUBE_RBAC_PROXY_IMAGE",
					"RELATED_IMAGE_OSE_PROM_LABEL_PROXY_IMAGE",
					"RELATED_IMAGE_PERSES_IMAGE",
				},
			},
		},
	}
}

// GetOperatorManifests renders the odh-observability chart. On RHOAI the
// monitoring operands namespace differs from the applications namespace, so
// the chart needs monitoringNamespace at render time (Tempo RBAC). That value
// is observability-only, so it is merged here rather than a generic
// ModuleConfig field.
func (h *handler) GetOperatorManifests(platform *modules.PlatformContext) modules.OperatorManifests {
	result := h.BaseHandler.GetOperatorManifests(platform)
	if platform == nil || platform.MonitoringNamespace == "" || len(result.HelmCharts) == 0 {
		return result
	}

	vals, err := result.HelmCharts[0].Values(context.Background())
	if err != nil {
		return result
	}
	if vals == nil {
		vals = map[string]any{}
	}
	vals[monitoringNamespaceHelmKey] = platform.MonitoringNamespace
	result.HelmCharts[0].Values = helm.Values(vals)
	return result
}

func (h *handler) PopulatePlatformModule(pm *configv1alpha1.PlatformModules, dscCtx *modules.DSCContext) {
	if pm == nil || dscCtx == nil || dscCtx.DSCI == nil {
		return
	}
	ms := dscCtx.DSCI.Spec.Monitoring.ManagementState
	if ms == "" {
		ms = operatorv1.Removed
	}
	pm.Monitoring.ManagementState = ms
}

func (h *handler) IsEnabled(modules *configv1alpha1.PlatformModules) bool {
	return modules != nil && modules.Monitoring.ManagementState == operatorv1.Managed
}

// BuildModuleCR constructs the Monitoring CR from DSCI spec with
// conditional field projection matching the monitoring domain rules:
// collector replica defaulting, TLS nulling when disabled, and
// metrics/traces omitted when storage/config is unset.
func (h *handler) BuildModuleCR(
	ctx context.Context,
	cli client.Client,
	dscCtx *modules.DSCContext,
	_ *modules.ModuleCRConfig,
) (*unstructured.Unstructured, error) {
	if dscCtx == nil || dscCtx.DSCI == nil {
		return nil, errors.New("DSCI is nil, cannot build monitoring CR")
	}

	spec := dscCtx.DSCI.Spec.Monitoring.MonitoringCommonSpec.DeepCopy()

	metricsEnabled := spec.Metrics != nil && (spec.Metrics.Storage != nil || len(spec.Metrics.Exporters) > 0)
	tracesEnabled := spec.Traces != nil

	if !metricsEnabled {
		spec.Metrics = nil
	}

	if tracesEnabled {
		if spec.Traces.TLS != nil && !spec.Traces.TLS.Enabled {
			spec.Traces.TLS = nil
		}
	} else {
		spec.Traces = nil
	}

	if metricsEnabled || tracesEnabled {
		if spec.CollectorReplicas == 0 {
			if cli != nil && cluster.IsSingleNodeCluster(ctx, cli) {
				spec.CollectorReplicas = 1
			} else {
				spec.CollectorReplicas = 2
			}
		}
	} else {
		spec.CollectorReplicas = 0
	}

	unstructuredSpec, err := runtime.DefaultUnstructuredConverter.ToUnstructured(spec)
	if err != nil {
		return nil, fmt.Errorf("failed to convert MonitoringSpec to unstructured: %w", err)
	}

	u := &unstructured.Unstructured{
		Object: map[string]any{
			"spec": unstructuredSpec,
		},
	}
	u.SetGroupVersionKind(h.Config.GVK)
	u.SetName(h.Config.CRName)

	return u, nil
}
