package monitoring

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

var componentRules = map[string]string{
	componentApi.DashboardComponentName:            "rhods-dashboard",
	componentApi.WorkbenchesComponentName:          "workbenches",
	componentApi.KueueComponentName:                "kueue",
	componentApi.CodeFlareComponentName:            "codeflare",
	componentApi.DataSciencePipelinesComponentName: "data-science-pipelines-operator",
	componentApi.ModelMeshServingComponentName:     "model-mesh",
	componentApi.RayComponentName:                  "ray",
	componentApi.TrustyAIComponentName:             "trustyai",
	componentApi.KserveComponentName:               "kserve",
	componentApi.TrainingOperatorComponentName:     "trainingoperator",
	componentApi.ModelRegistryComponentName:        "model-registry-operator",
	componentApi.ModelControllerComponentName:      "odh-model-controller",
	componentApi.FeastOperatorComponentName:        "feastoperator",
	componentApi.LlamaStackOperatorComponentName:   "llamastackoperator",
}

// initialize handles all pre-deployment configurations.
func initialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	// Only set prometheus configmap path
	rr.Manifests = []odhtypes.ManifestInfo{
		{
			Path:       odhdeploy.DefaultManifestPath,
			ContextDir: "monitoring/prometheus/apps",
		},
	}

	return nil
}

// if DSC has component as Removed, we remove component's Prom Rules.
// only when DSC has component as Managed and component CR is in "Ready" state, we add rules to Prom Rules.
// all other cases, we do not change Prom rules for component.
func updatePrometheusConfigMap(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	// Skip update prom config: if cluster is NOT ManagedRhoai
	if rr.Release.Name != cluster.ManagedRhoai {
		return nil
	}

	// Map component names to their rule prefixes
	dsc, err := cluster.GetDSC(ctx, rr.Client)
	if err != nil {
		if k8serr.IsNotFound(err) {
			// DSC doesn't exist, skip prometheus configmap update
			return nil
		}
		return fmt.Errorf("failed to retrieve DataScienceCluster: %w", err)
	}

	return cr.ForEach(func(ch cr.ComponentHandler) error {
		ci := ch.NewCRObject(dsc)
		ms := ch.GetManagementState(dsc) // check for modelcontroller with dependency is done in its GetManagementState()
		switch ms {
		case operatorv1.Removed: // remove
			return updatePrometheusConfig(ctx, false, componentRules[ch.GetName()])
		case operatorv1.Managed:
			ready, err := isComponentReady(ctx, rr.Client, ci)
			if err != nil {
				return fmt.Errorf("failed to get component status %w", err)
			}
			if !ready { // not ready, skip change on prom rules
				return nil
			}
			// add
			return updatePrometheusConfig(ctx, true, componentRules[ch.GetName()])
		default:
			return fmt.Errorf("unsupported management state %s", ms)
		}
	})
}

func createMonitoringStack(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *services.Monitoring")
	}

	if monitoring.Spec.Metrics != nil && (monitoring.Spec.Metrics.Resources != nil || monitoring.Spec.Metrics.Storage != nil) {
		if msExists, _ := cluster.HasCRD(ctx, rr.Client, gvk.MonitoringStack); !msExists {
			// CRD not available, skip monitoring stack deployment (this is expected when monitoring stack operator is not installed)
			rr.Conditions.MarkFalse(
				status.ConditionMonitoringStackAvailable,
				conditions.WithReason(status.MonitoringStackOperatorMissingReason),
				conditions.WithMessage(status.MonitoringStackOperatorMissingMessage),
			)
			return nil
		}

		rr.Conditions.MarkTrue(status.ConditionMonitoringStackAvailable)

		template := []odhtypes.TemplateInfo{
			{
				FS:   resourcesFS,
				Path: MonitoringStackTemplate,
			},
			{
				FS:   resourcesFS,
				Path: PrometheusRouteTemplate,
			},
		}

		rr.Templates = append(rr.Templates, template...)

		return nil
	}

	return nil
}

func createOpenTelemetryCollector(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	otcExists, _ := cluster.HasCRD(ctx, rr.Client, gvk.OpenTelemetryCollector)
	if !otcExists {
		rr.Conditions.MarkFalse(
			status.ConditionOpenTelemetryCollectorAvailable,
			conditions.WithReason(status.OpenTelemetryCollectorCRDNotFoundReason),
			conditions.WithMessage(status.OpenTelemetryCollectorCRDNotFoundMessage),
		)
		return nil
	}

	// Mark OpenTelemetryCollector CRD as available when CRD exists
	rr.Conditions.MarkTrue(
		status.ConditionOpenTelemetryCollectorAvailable,
		conditions.WithReason(status.OpenTelemetryCollectorCRDAvailableReason),
		conditions.WithMessage(status.OpenTelemetryCollectorCRDAvailableMessage),
	)

	mon, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *services.Monitoring")
	}

	if mon.Spec.Metrics != nil {
		template := []odhtypes.TemplateInfo{
			{
				FS:   resourcesFS,
				Path: OpenTelemetryCollectorTemplate,
			},
			{
				FS:   resourcesFS,
				Path: CollectorRBACTemplate,
			},
			{
				FS:   resourcesFS,
				Path: CollectorServiceMonitorsTemplate,
			},
		}
		rr.Templates = append(rr.Templates, template...)
	} else {
		// No metrics configuration or metrics not sufficiently configured
		rr.Conditions.MarkFalse(
			status.ConditionMonitoringStackAvailable,
			conditions.WithReason(status.MetricsNotConfiguredReason),
			conditions.WithMessage(status.MetricsNotConfiguredMessage),
		)
	}

	return nil
}

// deployTempo creates Tempo resources based on the Monitoring CR configuration.
func deployTempo(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *services.Monitoring")
	}

	// Read traces configuration directly from Monitoring CR
	if monitoring.Spec.Traces == nil {
		// No traces configuration - GC action will clean up any existing Tempo resources
		rr.Conditions.MarkFalse(
			status.ConditionTempoAvailable,
			conditions.WithReason(status.TracesNotConfiguredReason),
			conditions.WithMessage(status.TracesNotConfiguredMessage),
		)
		return nil
	}

	traces := monitoring.Spec.Traces

	var requiredCRD schema.GroupVersionKind
	if traces.Storage.Backend == "pv" {
		requiredCRD = gvk.TempoMonolithic
	} else {
		requiredCRD = gvk.TempoStack
	}

	crdExists, err := cluster.HasCRD(ctx, rr.Client, requiredCRD)
	if err != nil {
		return fmt.Errorf("failed to check if CRD exists: %w", err)
	}
	if !crdExists {
		// CRD not available, skip tempo deployment (this is expected when tempo operator is not installed)
		rr.Conditions.MarkFalse(
			status.ConditionTempoAvailable,
			conditions.WithReason(status.TempoOperatorMissingReason),
			conditions.WithMessage(status.TempoOperatorMissingMessage),
		)
		return nil
	}

	rr.Conditions.MarkTrue(status.ConditionTempoAvailable)

	// Add the appropriate template based on backend type
	if traces.Storage.Backend == "pv" {
		template := []odhtypes.TemplateInfo{
			{
				FS:   resourcesFS,
				Path: TempoMonolithicTemplate,
			},
		}
		rr.Templates = append(rr.Templates, template...)
	} else {
		template := []odhtypes.TemplateInfo{
			{
				FS:   resourcesFS,
				Path: TempoStackTemplate,
			},
		}
		rr.Templates = append(rr.Templates, template...)
	}

	return nil
}
