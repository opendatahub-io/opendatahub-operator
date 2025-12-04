package monitoring

import (
	"context"
	"embed"
	"errors"
	"fmt"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	MonitoringStackTemplate                       = "resources/monitoring-stack.tmpl.yaml"
	MonitoringStackAlertmanagerRBACTemplate       = "resources/monitoringstack-alertmanager-rbac.tmpl.yaml"
	TempoMonolithicTemplate                       = "resources/tempo-monolithic.tmpl.yaml"
	TempoStackTemplate                            = "resources/tempo-stack.tmpl.yaml"
	OpenTelemetryCollectorTemplate                = "resources/opentelemetry-collector.tmpl.yaml"
	CollectorServiceMonitorsTemplate              = "resources/collector-servicemonitors.tmpl.yaml"
	CollectorPrometheusServiceTemplate            = "resources/collector-prometheus-service.tmpl.yaml"
	CollectorRBACTemplate                         = "resources/collector-rbac.tmpl.yaml"
	PrometheusRouteTemplate                       = "resources/data-science-prometheus-route.tmpl.yaml"
	InstrumentationTemplate                       = "resources/instrumentation.tmpl.yaml"
	PrometheusNamespaceProxyTemplate              = "resources/data-science-prometheus-namespace-proxy.tmpl.yaml"
	PrometheusNamespaceProxyNetworkPolicyTemplate = "resources/data-science-prometheus-namespace-proxy-network-policy.tmpl.yaml"
	PrometheusServiceOverrideTemplate             = "resources/data-science-prometheus-service-override.tmpl.yaml"
	PrometheusNetworkPolicyTemplate               = "resources/data-science-prometheus-network-policy.tmpl.yaml"
	PrometheusWebTLSServiceTemplate               = "resources/prometheus-web-tls-service.tmpl.yaml"
	ThanosQuerierTemplate                         = "resources/thanos-querier-cr.tmpl.yaml"
	ThanosQuerierRouteTemplate                    = "resources/thanos-querier-route.tmpl.yaml"
	PersesTemplate                                = "resources/perses.tmpl.yaml"
	PersesTempoDatasourceTemplate                 = "resources/perses-tempo-datasource.tmpl.yaml"
	PersesTempoDashboardTemplate                  = "resources/perses-tempo-dashboard.tmpl.yaml"
	PersesDatasourcePrometheusTemplate            = "resources/perses-datasource-prometheus.tmpl.yaml"
	PrometheusClusterProxyTemplate                = "resources/data-science-prometheus-cluster-proxy.tmpl.yaml"

	// Resource names.
	PersesTempoDatasourceName = "tempo-datasource"
	PersesTempoDashboardName  = "data-science-tempo-traces"
)

// CRDRequirement defines a required CRD and its associated condition for monitoring components.
type CRDRequirement struct {
	GVK           schema.GroupVersionKind
	ConditionType string
}

var componentRules = map[string]string{
	componentApi.DashboardComponentName:            "rhods-dashboard",
	componentApi.WorkbenchesComponentName:          "workbenches",
	componentApi.KueueComponentName:                "kueue",
	componentApi.DataSciencePipelinesComponentName: "data-science-pipelines-operator",
	componentApi.RayComponentName:                  "ray",
	componentApi.TrustyAIComponentName:             "trustyai",
	componentApi.KserveComponentName:               "kserve",
	componentApi.TrainingOperatorComponentName:     "trainingoperator",
	componentApi.TrainerComponentName:              "trainer",
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

// validateRequiredCRDs checks multiple CRDs for atomic deployment.
// For atomic deployment: if ANY CRD is missing, ALL conditions are set to False.
// Returns true if all CRDs exist, false otherwise.
func validateRequiredCRDs(ctx context.Context, rr *odhtypes.ReconciliationRequest, requirements []CRDRequirement) bool {
	allExist := true

	for _, req := range requirements {
		exists, err := cluster.HasCRD(ctx, rr.Client, req.GVK)
		if err != nil {
			return false // or handle error appropriately
		}
		if !exists {
			allExist = false
		}
	}

	// If any CRD is missing, set ALL conditions to False (atomic deployment)
	if !allExist {
		for _, req := range requirements {
			setConditionFalse(rr, req.ConditionType,
				req.GVK.Kind+"CRDNotFoundReason",
				fmt.Sprintf("%s CRD Not Found (atomic deployment requires all CRDs)", req.GVK.Kind))
		}
	}

	return allExist
}

// setConditionFalse sets a condition to False with the specified reason and message.
// This helper reduces code duplication and ensures uniform condition handling across
// all monitoring components for various failure scenarios (missing CRDs, not managed, not configured, etc.).
func setConditionFalse(rr *odhtypes.ReconciliationRequest, conditionType, reason, message string) {
	rr.Conditions.MarkFalse(
		conditionType,
		conditions.WithReason(reason),
		conditions.WithMessage("%s", message),
	)
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

// deployMonitoringStackWithQuerierAndRestrictions handles deployment of MonitoringStack and ThanosQuerier components.
// These components are deployed together as ThanosQuerier depends on MonitoringStack for proper functioning.
func deployMonitoringStackWithQuerierAndRestrictions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *services.Monitoring")
	}

	// Early exit if no metrics configuration
	if monitoring.Spec.Metrics == nil {
		setConditionFalse(rr, status.ConditionMonitoringStackAvailable, status.MetricsNotConfiguredReason, status.MetricsNotConfiguredMessage)
		setConditionFalse(rr, status.ConditionThanosQuerierAvailable, status.MetricsNotConfiguredReason, status.MetricsNotConfiguredMessage)
		return nil
	}

	// Define required CRDs and their corresponding conditions for validation
	requirements := []CRDRequirement{
		{GVK: gvk.MonitoringStack, ConditionType: status.ConditionMonitoringStackAvailable},
		{GVK: gvk.ThanosQuerier, ConditionType: status.ConditionThanosQuerierAvailable},
	}

	// Skip deployment if any required CRD is missing
	if !validateRequiredCRDs(ctx, rr, requirements) {
		return nil
	}

	// All prerequisites met, mark all components as available and deploy
	rr.Conditions.MarkTrue(status.ConditionMonitoringStackAvailable)
	rr.Conditions.MarkTrue(status.ConditionThanosQuerierAvailable)

	// Prepare and deploy all component templates atomically
	templates := []odhtypes.TemplateInfo{
		{FS: resourcesFS, Path: PrometheusWebTLSServiceTemplate},
		{FS: resourcesFS, Path: MonitoringStackTemplate},
		{FS: resourcesFS, Path: MonitoringStackAlertmanagerRBACTemplate},
		{FS: resourcesFS, Path: PrometheusRouteTemplate},
		{FS: resourcesFS, Path: PrometheusServiceOverrideTemplate},
		{FS: resourcesFS, Path: PrometheusNetworkPolicyTemplate},
		{FS: resourcesFS, Path: PrometheusNamespaceProxyTemplate},
		{FS: resourcesFS, Path: PrometheusNamespaceProxyNetworkPolicyTemplate},
		{FS: resourcesFS, Path: ThanosQuerierTemplate},
		{FS: resourcesFS, Path: ThanosQuerierRouteTemplate},
	}

	// Deploy all components atomically with the same generation annotation
	rr.Templates = append(rr.Templates, templates...)
	return nil
}

// deployTracingStack handles deployment of both Tempo and Instrumentation components.
// These components work together for distributed tracing - Tempo stores traces while
// Instrumentation configures auto-instrumentation for applications.
func deployTracingStack(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *services.Monitoring")
	}

	// Early exit if no traces configuration - both components require traces to be configured
	if monitoring.Spec.Traces == nil {
		setConditionFalse(rr, status.ConditionTempoAvailable,
			status.TracesNotConfiguredReason, status.TracesNotConfiguredMessage)
		setConditionFalse(rr, status.ConditionInstrumentationAvailable,
			status.TracesNotConfiguredReason, status.TracesNotConfiguredMessage)
		return nil
	}

	traces := monitoring.Spec.Traces

	// Determine required Tempo CRD based on storage backend
	var tempoCRD schema.GroupVersionKind
	var tempoTemplate string
	if traces.Storage.Backend == "pv" {
		tempoCRD = gvk.TempoMonolithic
		tempoTemplate = TempoMonolithicTemplate
	} else {
		tempoCRD = gvk.TempoStack
		tempoTemplate = TempoStackTemplate
	}

	// Define required CRDs for both tracing components
	requirements := []CRDRequirement{
		{GVK: tempoCRD, ConditionType: status.ConditionTempoAvailable},
		{GVK: gvk.Instrumentation, ConditionType: status.ConditionInstrumentationAvailable},
	}

	// Skip deployment if any required CRD is missing
	if !validateRequiredCRDs(ctx, rr, requirements) {
		return nil
	}

	// All prerequisites met, mark both components as available and deploy
	rr.Conditions.MarkTrue(status.ConditionTempoAvailable)
	rr.Conditions.MarkTrue(status.ConditionInstrumentationAvailable)

	templates := []odhtypes.TemplateInfo{
		{FS: resourcesFS, Path: tempoTemplate},
		{FS: resourcesFS, Path: InstrumentationTemplate},
	}

	rr.Templates = append(rr.Templates, templates...)
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
			conditions.WithMessage("%s CRD Not Found", gvk.OpenTelemetryCollector.Kind),
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
		// ServiceMonitors (always deployed for collector health monitoring)
		// Note: The template contains conditional logic that only renders the
		// data-science-prometheus-monitor ServiceMonitor when .Metrics != nil
		{
			FS:   resourcesFS,
			Path: CollectorServiceMonitorsTemplate,
		},
	}

	// Prometheus Service with TLS (only when metrics collection is enabled)
	if monitoring.Spec.Metrics != nil {
		template = append(template, odhtypes.TemplateInfo{
			FS:   resourcesFS,
			Path: CollectorPrometheusServiceTemplate,
		})
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
			conditions.WithMessage("%s CRD Not Found", gvk.PrometheusRule.Kind),
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

func deployPerses(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *services.Monitoring")
	}

	if monitoring.Spec.Metrics == nil && monitoring.Spec.Traces == nil {
		setConditionFalse(rr, status.ConditionPersesAvailable,
			status.MetricsNotConfiguredReason+"And"+status.TracesNotConfiguredReason,
			"Perses requires at least Metrics or Traces to be configured")
		return nil
	}

	// Check for required CRDs
	requirements := []CRDRequirement{
		{GVK: gvk.Perses, ConditionType: status.ConditionPersesAvailable},
	}

	if !validateRequiredCRDs(ctx, rr, requirements) {
		return nil
	}

	rr.Conditions.MarkTrue(status.ConditionPersesAvailable)

	template := []odhtypes.TemplateInfo{
		{
			FS:   resourcesFS,
			Path: PersesTemplate,
		},
	}
	rr.Templates = append(rr.Templates, template...)

	return nil
}

func deployPersesTempoIntegration(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *services.Monitoring")
	}

	// Check if PersesDatasource CRD exists first
	persesDatasourceExists, err := cluster.HasCRD(ctx, rr.Client, gvk.PersesDatasource)
	if err != nil {
		return fmt.Errorf("failed to check if PersesDatasource CRD exists: %w", err)
	}

	// Check if PersesDashboard CRD exists
	persesDashboardExists, err := cluster.HasCRD(ctx, rr.Client, gvk.PersesDashboard)
	if err != nil {
		return fmt.Errorf("failed to check if PersesDashboard CRD exists: %w", err)
	}

	// Only create Perses datasource if traces are configured
	if monitoring.Spec.Traces == nil {
		// Clean up existing datasource if its CRD exists
		if persesDatasourceExists {
			// Delete datasource
			datasource := &unstructured.Unstructured{}
			datasource.SetGroupVersionKind(gvk.PersesDatasource)
			datasource.SetName(PersesTempoDatasourceName)
			datasource.SetNamespace(monitoring.Spec.Namespace)

			if err := rr.Client.Delete(ctx, datasource); err != nil {
				if !k8serr.IsNotFound(err) {
					return fmt.Errorf("failed to delete PersesDatasource: %w", err)
				}
			}
		}

		// Clean up existing dashboard if its CRD exists
		if persesDashboardExists {
			// Delete dashboard
			dashboard := &unstructured.Unstructured{}
			dashboard.SetGroupVersionKind(gvk.PersesDashboard)
			dashboard.SetName(PersesTempoDashboardName)
			dashboard.SetNamespace(monitoring.Spec.Namespace)

			if err := rr.Client.Delete(ctx, dashboard); err != nil {
				if !k8serr.IsNotFound(err) {
					return fmt.Errorf("failed to delete PersesDashboard: %w", err)
				}
			}
		}

		rr.Conditions.MarkFalse(
			status.ConditionPersesTempoDataSourceAvailable,
			conditions.WithReason(status.TracesNotConfiguredReason),
			conditions.WithMessage(status.TracesNotConfiguredMessage),
		)
		return nil
	}

	if !persesDatasourceExists {
		rr.Conditions.MarkFalse(
			status.ConditionPersesTempoDataSourceAvailable,
			conditions.WithReason(gvk.PersesDatasource.Kind+"CRDNotFoundReason"),
			conditions.WithMessage("%s CRD Not Found", gvk.PersesDatasource.Kind),
		)
		return nil
	}

	rr.Conditions.MarkTrue(status.ConditionPersesTempoDataSourceAvailable)

	// Deploy datasource template (always deploy if CRD exists)
	templates := []odhtypes.TemplateInfo{
		{
			FS:   resourcesFS,
			Path: PersesTempoDatasourceTemplate,
		},
	}

	// Only deploy dashboard template if its CRD exists
	if persesDashboardExists {
		templates = append(templates, odhtypes.TemplateInfo{
			FS:   resourcesFS,
			Path: PersesTempoDashboardTemplate,
		})
	}

	rr.Templates = append(rr.Templates, templates...)

	return nil
}

func deployPersesPrometheusIntegration(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *services.Monitoring")
	}

	if monitoring.Spec.Metrics == nil {
		setConditionFalse(rr, status.ConditionPersesPrometheusDataSourceAvailable,
			status.MetricsNotConfiguredReason,
			"Prometheus datasource requires metrics configuration")
		return nil
	}

	datasourceExists, err := cluster.HasCRD(ctx, rr.Client, gvk.PersesDatasource)
	if err != nil {
		return fmt.Errorf("failed to check if CRD PersesDatasource exists: %w", err)
	}
	if !datasourceExists {
		setConditionFalse(rr, status.ConditionPersesPrometheusDataSourceAvailable,
			gvk.PersesDatasource.Kind+"CRDNotFoundReason",
			fmt.Sprintf("%s CRD Not Found", gvk.PersesDatasource.Kind))
		return nil
	}

	rr.Conditions.MarkTrue(status.ConditionPersesPrometheusDataSourceAvailable)

	templates := []odhtypes.TemplateInfo{
		{
			FS:   resourcesFS,
			Path: PersesDatasourcePrometheusTemplate,
		},
	}
	rr.Templates = append(rr.Templates, templates...)

	return nil
}

func deployNodeMetricsEndpoint(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *services.Monitoring")
	}

	if monitoring.Spec.Metrics == nil {
		rr.Conditions.MarkFalse(
			status.ConditionNodeMetricsEndpointAvailable,
			conditions.WithReason(status.MetricsNotConfiguredReason),
			conditions.WithMessage(status.MetricsNotConfiguredMessage),
		)
		return nil
	}

	rr.Conditions.MarkTrue(status.ConditionNodeMetricsEndpointAvailable)

	templates := []odhtypes.TemplateInfo{
		{
			FS:   resourcesFS,
			Path: PrometheusClusterProxyTemplate,
		},
	}

	rr.Templates = append(rr.Templates, templates...)

	return nil
}
