package monitoring

import (
	"context"
	"embed"
	"errors"
	"fmt"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

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

const (
	// Template files.
	MonitoringStackTemplate          = "resources/monitoring-stack.tmpl.yaml"
	TempoMonolithicTemplate          = "resources/tempo-monolithic.tmpl.yaml"
	TempoStackTemplate               = "resources/tempo-stack.tmpl.yaml"
	OpenTelemetryCollectorTemplate   = "resources/opentelemetry-collector.tmpl.yaml"
	CollectorServiceMonitorsTemplate = "resources/collector-servicemonitors.tmpl.yaml"
	CollectorRBACTemplate            = "resources/collector-rbac.tmpl.yaml"
	PrometheusRouteTemplate          = "resources/prometheus-route.tmpl.yaml"
	InstrumentationTemplate          = "resources/instrumentation.tmpl.yaml"
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

//go:embed resources
//go:embed monitoring
var resourcesFS embed.FS

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
		if ch.IsEnabled(dsc) {
			ready, err := isComponentReady(ctx, rr.Client, ci)
			if err != nil {
				return fmt.Errorf("failed to get component status %w", err)
			}
			if !ready { // not ready, skip change on prom rules
				return nil
			}
			// add
			return updatePrometheusConfig(ctx, true, componentRules[ch.GetName()])
		} else {
			return updatePrometheusConfig(ctx, false, componentRules[ch.GetName()])
		}
	})
}

func deployMonitoringStack(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *services.Monitoring")
	}

	// No monitoring stack configuration
	if monitoring.Spec.Metrics == nil {
		rr.Conditions.MarkFalse(
			status.ConditionMonitoringStackAvailable,
			conditions.WithReason(status.MetricsNotConfiguredReason),
			conditions.WithMessage(status.MetricsNotConfiguredMessage),
		)
		return nil
	}

	msExists, err := cluster.HasCRD(ctx, rr.Client, gvk.MonitoringStack)
	if err != nil {
		return fmt.Errorf("failed to check if CRD MonitoringStack exists: %w", err)
	}
	if !msExists {
		// CRD not available, skip monitoring stack deployment (this is expected when monitoring stack operator is not installed)
		rr.Conditions.MarkFalse(
			status.ConditionMonitoringStackAvailable,
			conditions.WithReason(gvk.MonitoringStack.Kind+"CRDNotFoundReason"),
			conditions.WithMessage(gvk.MonitoringStack.Kind+" CRD Not Found"),
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

func deployOpenTelemetryCollector(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *services.Monitoring")
	}

	// Read metrics and traces configuration directly from Monitoring CR
	if monitoring.Spec.Metrics == nil && monitoring.Spec.Traces == nil {
		// No metrics and traces configuration - skip OpenTelemetry collector deployment
		rr.Conditions.MarkFalse(
			status.ConditionOpenTelemetryCollectorAvailable,
			conditions.WithReason(status.MetricsNotConfiguredReason+"And"+status.TracesNotConfiguredReason),
			conditions.WithMessage(status.MetricsNotConfiguredMessage+"\n"+status.TracesNotConfiguredMessage),
		)
		return nil
	}

	otcExists, err := cluster.HasCRD(ctx, rr.Client, gvk.OpenTelemetryCollector)
	if err != nil {
		return fmt.Errorf("failed to check if CRD OpenTelemetryCollector exists: %w", err)
	}
	if !otcExists {
		rr.Conditions.MarkFalse(
			status.ConditionOpenTelemetryCollectorAvailable,
			conditions.WithReason(gvk.OpenTelemetryCollector.Kind+"CRDNotFoundReason"),
			conditions.WithMessage(gvk.OpenTelemetryCollector.Kind+" CRD Not Found"),
		)
		return nil
	}

	// Mark OpenTelemetryCollector CRD as available when CRD exists
	rr.Conditions.MarkTrue(
		status.ConditionOpenTelemetryCollectorAvailable,
	)

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
	var templatePath string
	if traces.Storage.Backend == "pv" {
		requiredCRD = gvk.TempoMonolithic
		templatePath = TempoMonolithicTemplate
	} else {
		requiredCRD = gvk.TempoStack
		templatePath = TempoStackTemplate
	}

	crdExists, err := cluster.HasCRD(ctx, rr.Client, requiredCRD)
	if err != nil {
		return fmt.Errorf("failed to check if CRD exists: %w", err)
	}
	if !crdExists {
		// CRD not available, skip tempo deployment (this is expected when tempo operator is not installed)
		rr.Conditions.MarkFalse(
			status.ConditionTempoAvailable,
			conditions.WithReason(requiredCRD.Kind+"CRDNotFoundReason"),
			conditions.WithMessage(requiredCRD.Kind+" CRD Not Found"),
		)
		return nil
	}

	rr.Conditions.MarkTrue(status.ConditionTempoAvailable)

	template := []odhtypes.TemplateInfo{
		{
			FS:   resourcesFS,
			Path: templatePath,
		},
	}
	rr.Templates = append(rr.Templates, template...)

	return nil
}

// deployInstrumentation manages OpenTelemetry Instrumentation CRs using templates.
func deployInstrumentation(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *serviceApi.Monitoring")
	}

	// Only create instrumentation CR if traces are configured
	if monitoring.Spec.Traces == nil {
		// If traces are not configured, GC will clean up any existing instrumentation CRs
		rr.Conditions.MarkFalse(
			status.ConditionInstrumentationAvailable,
			conditions.WithReason(status.TracesNotConfiguredReason),
			conditions.WithMessage(status.TracesNotConfiguredMessage),
		)
		return nil
	}

	// Traces are configured, check if Instrumentation CRD exists before creating the template
	instrumentationCRDExists, err := cluster.HasCRD(ctx, rr.Client, gvk.Instrumentation)
	if err != nil {
		return fmt.Errorf("failed to check if Instrumentation CRD exists: %w", err)
	}
	if !instrumentationCRDExists {
		rr.Conditions.MarkFalse(
			status.ConditionInstrumentationAvailable,
			conditions.WithReason(gvk.Instrumentation.Kind+"CRDNotFoundReason"),
			conditions.WithMessage(gvk.Instrumentation.Kind+" CRD Not Found"),
		)
		return nil
	}

	rr.Conditions.MarkTrue(status.ConditionInstrumentationAvailable)

	// Add instrumentation template to be rendered
	template := []odhtypes.TemplateInfo{
		{
			FS:   resourcesFS,
			Path: InstrumentationTemplate,
		},
	}
	rr.Templates = append(rr.Templates, template...)

	return nil
}

func deployAlerting(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *services.Monitoring")
	}

	if monitoring.Spec.Alerting == nil {
		rr.Conditions.MarkFalse(
			status.ConditionAlertingAvailable,
			conditions.WithReason(status.AlertingNotConfiguredReason),
			conditions.WithMessage(status.AlertingNotConfiguredMessage),
		)
		return nil
	}

	// Check required CRD for alerting
	exists, err := cluster.HasCRD(ctx, rr.Client, gvk.PrometheusRule)
	if err != nil {
		return fmt.Errorf("failed to check if %s CRD exists: %w", gvk.PrometheusRule.Kind, err)
	}
	if !exists {
		rr.Conditions.MarkFalse(
			status.ConditionAlertingAvailable,
			conditions.WithReason(gvk.PrometheusRule.Kind+"CRDNotFoundReason"),
			conditions.WithMessage(gvk.PrometheusRule.Kind+" CRD Not Found"),
		)
		return nil
	}

	rr.Conditions.MarkTrue(status.ConditionAlertingAvailable)
	// Add operator prometheus rules, we can deploy operator alerts without any components
	templates := []odhtypes.TemplateInfo{
		{
			FS:   resourcesFS,
			Path: "monitoring/operator-prometheusrules.tmpl.yaml",
		},
	}
	rr.Templates = append(rr.Templates, templates...)

	dsc, err := cluster.GetDSC(ctx, rr.Client)
	if err != nil {
		if k8serr.IsNotFound(err) {
			// DSC doesn't exist
			return nil
		}
		return fmt.Errorf("failed to retrieve DataScienceCluster: %w", err)
	}

	// Add component prometheus rules for each enabled and ready component.
	// Collect errors for each component and report them at the end.
	// Component A could succeed, component B could fail and component C could succeed.
	// Log which components actually failed rather than just bailing out early.
	var addErrors []error
	var cleanupErrors []error

	forEachErr := cr.ForEach(func(ch cr.ComponentHandler) error {
		componentName := ch.GetName()
		ci := ch.NewCRObject(dsc)

		if ch.IsEnabled(dsc) {
			ready, err := isComponentReady(ctx, rr.Client, ci)
			if err != nil {
				addErrors = append(addErrors, fmt.Errorf("failed to get status for component %s: %w", componentName, err))
				return nil // Continue processing other components
			}
			if !ready {
				return nil
			}
			// component is ready, add alerting rules
			if err := addPrometheusRules(componentName, rr); err != nil {
				addErrors = append(addErrors, fmt.Errorf("failed to add prometheus rules for component %s: %w", componentName, err))
				return nil // Continue processing other components
			}
		} else {
			// component is not enabled, check if prometheus rules exist and cleanup if they do
			if err := cleanupPrometheusRules(ctx, componentName, rr); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("failed to cleanup prometheus rules for component %s: %w", componentName, err))
				return nil // Continue processing other components
			}
		}
		return nil
	})

	// Handle registry iteration errors separately - something is wrong with the component registry itself
	if forEachErr != nil {
		return fmt.Errorf("failed to iterate components: %w", forEachErr)
	}

	// If we fail to add prometheus rules for a component.
	if len(addErrors) > 0 {
		// Log errors but don't fail the reconciliation
		for _, addErr := range addErrors {
			logf.FromContext(ctx).Error(addErr, "Failed to add prometheus rules for component")
		}
	}

	// If we fail to clean up prometheus rules for a component.
	if len(cleanupErrors) > 0 {
		// Log errors but don't fail the reconciliation
		for _, cleanupErr := range cleanupErrors {
			logf.FromContext(ctx).Error(cleanupErr, "Failed to cleanup prometheus rules for component")
		}
	}

	if len(addErrors) > 0 || len(cleanupErrors) > 0 {
		return errors.New("errors occurred while adding or cleaning up prometheus rules for components")
	}

	return nil
}
