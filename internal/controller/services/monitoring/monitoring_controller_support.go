package monitoring

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/go-multierror"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	componentMonitoring "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	// Dependent operators names. match the one in the operatorcondition..
	opentelemetryOperator        = "opentelemetry-operator"
	clusterObservabilityOperator = "cluster-observability-operator"
	tempoOperator                = "tempo-operator"

	defaultCPULimit      = "500m"
	defaultMemoryLimit   = "512Mi"
	defaultCPURequest    = "100m"
	defaultMemoryRequest = "256Mi"
	defaultStorageSize   = "5Gi"
	defaultRetention     = "90d"
)

var componentIDRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(?:/[A-Za-z0-9][A-Za-z0-9_-]*)?$`)

func isReservedName(n string) bool {
	reservedNames := map[string]bool{
		"otlp/tempo": true,
		"prometheus": true,
	}
	return reservedNames[n]
}

func validateExporters(exporters map[string]runtime.RawExtension) (map[string]string, error) {
	validatedExporters := make(map[string]string)

	for name, rawConfig := range exporters {
		if isReservedName(name) {
			return nil, fmt.Errorf("exporter name '%s' is reserved and cannot be used", name)
		}

		if !componentIDRE.MatchString(name) {
			return nil, fmt.Errorf(
				"invalid exporter name '%s': must match OpenTelemetry component ID format %q",
				name, componentIDRE.String(),
			)
		}

		// Obtain raw bytes from Raw or Object
		var raw []byte
		switch {
		case len(rawConfig.Raw) > 0:
			raw = rawConfig.Raw
		case rawConfig.Object != nil:
			b, err := yaml.Marshal(rawConfig.Object)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal exporter object for '%s': %w", name, err)
			}
			raw = b
		default:
			// nothing to process
			continue
		}

		// Convert RawExtension to a map for validation and YAML conversion
		var config map[string]interface{}
		if err := yaml.Unmarshal(raw, &config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal exporter config for '%s': %w", name, err)
		}

		// Convert config back to YAML string for template rendering
		configYAML, err := yaml.Marshal(config)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal exporter config for '%s': %w", name, err)
		}
		// Store the YAML string for template rendering with the indent template function
		validatedExporters[name] = strings.TrimSpace(string(configYAML))
	}

	return validatedExporters, nil
}

func addTracesTemplateData(templateData map[string]any, traces *serviceApi.Traces, namespace string) error {
	templateData["OtlpEndpoint"] = fmt.Sprintf("http://data-science-collector.%s.svc.cluster.local:4317", namespace)
	templateData["SampleRatio"] = traces.SampleRatio
	templateData["Backend"] = traces.Storage.Backend // backend has default "pv" set in API

	// Add retention for all backends (both TempoMonolithic and TempoStack)
	templateData["TracesRetention"] = traces.Storage.Retention.Duration.String()

	// Add tempo-related data from traces.Storage fields (Storage is a struct, not a pointer)
	switch traces.Storage.Backend {
	case "pv":
		templateData["TempoEndpoint"] = fmt.Sprintf("tempo-data-science-tempomonolithic.%s.svc.cluster.local:4317", namespace)
		templateData["Size"] = traces.Storage.Size
	case "s3", "gcs":
		templateData["TempoEndpoint"] = fmt.Sprintf("tempo-data-science-tempostack-gateway.%s.svc.cluster.local:4317", namespace)
		templateData["Secret"] = traces.Storage.Secret
	}

	// Validate and add custom exporters
	// Always initialize validatedExporters to avoid template rendering failures
	validatedExporters := make(map[string]string)
	exporterNames := make([]string, 0)
	if traces.Exporters != nil {
		var err error
		validatedExporters, err = validateExporters(traces.Exporters)
		if err != nil {
			return err
		}
		for n := range validatedExporters {
			exporterNames = append(exporterNames, n)
		}
		sort.Strings(exporterNames)
	}
	// Always set TracesExporters, even if empty, to prevent template rendering failures
	templateData["TracesExporters"] = validatedExporters
	templateData["TracesExporterNames"] = exporterNames

	return nil
}

func getTemplateData(ctx context.Context, rr *odhtypes.ReconciliationRequest) (map[string]any, error) {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return nil, errors.New("instance is not of type services.Monitoring")
	}

	templateData := map[string]any{
		"Namespace":            monitoring.Spec.Namespace,
		"Traces":               monitoring.Spec.Traces != nil,
		"Metrics":              monitoring.Spec.Metrics != nil,
		"AcceleratorMetrics":   monitoring.Spec.Metrics != nil,
		"ApplicationNamespace": rr.DSCI.Spec.ApplicationsNamespace,
		"MetricsExporters":     make(map[string]interface{}),
		"MetricsExporterNames": []string{},
	}

	// Add metrics-related data if metrics are configured
	if metrics := monitoring.Spec.Metrics; metrics != nil {
		if err := addMetricsData(ctx, rr, metrics, templateData); err != nil {
			return nil, err
		}
	}

	// Add traces-related data if traces are configured
	if traces := monitoring.Spec.Traces; traces != nil {
		addTracesData(traces, monitoring.Spec.Namespace, templateData)
		if err := addTracesTemplateData(templateData, traces, monitoring.Spec.Namespace); err != nil {
			return nil, err
		}
	}

	return templateData, nil
}

// isSingleNodeCluster determines if the cluster is a single-node cluster by counting the actual nodes.
func isSingleNodeCluster(ctx context.Context, rr *odhtypes.ReconciliationRequest) bool {
	nodeList := &corev1.NodeList{}
	if err := rr.Client.List(ctx, nodeList); err != nil {
		logf.FromContext(ctx).Info("could not list nodes, defaulting to multi-node behavior", "error", err)
		return false
	}

	// Count only nodes that are ready and not marked for deletion
	// We only need to know if there are more than 1 ready nodes, so we can break early
	var readyNodeCount int
	for _, node := range nodeList.Items {
		if node.DeletionTimestamp == nil {
			for _, condition := range node.Status.Conditions {
				if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
					readyNodeCount++
					break
				}
			}
		}
		if readyNodeCount > 1 {
			break
		}
	}

	logf.FromContext(ctx).V(1).Info("detected cluster size", "totalNodes", len(nodeList.Items), "readyNodes", readyNodeCount)
	return readyNodeCount <= 1
}

func addMonitoringCapability(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	// Set initial condition state
	rr.Conditions.MarkUnknown(status.ConditionMonitoringAvailable)

	if err := checkMonitoringPreconditions(ctx, rr); err != nil {
		log.Error(err, "Monitoring preconditions failed")

		rr.Conditions.MarkFalse(
			status.ConditionMonitoringAvailable,
			cond.WithReason(status.MissingOperatorReason),
			cond.WithMessage("Monitoring preconditions failed: %s", err.Error()),
		)

		return err
	}

	rr.Conditions.MarkTrue(status.ConditionMonitoringAvailable)

	return nil
}

func checkMonitoringPreconditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type services.Monitoring")
	}
	var allErrors *multierror.Error

	// Check for opentelemetry-product operator if either metrics or traces are enabled
	if monitoring.Spec.Metrics != nil || monitoring.Spec.Traces != nil {
		if found, err := cluster.OperatorExists(ctx, rr.Client, opentelemetryOperator); err != nil || !found {
			if err != nil {
				return odherrors.NewStopErrorW(err)
			}
			allErrors = multierror.Append(allErrors, odherrors.NewStopError(status.OpenTelemetryCollectorOperatorMissingMessage))
		}
	}

	// Check for cluster-observability-operator if metrics are enabled
	if monitoring.Spec.Metrics != nil {
		if found, err := cluster.OperatorExists(ctx, rr.Client, clusterObservabilityOperator); err != nil || !found {
			if err != nil {
				return odherrors.NewStopErrorW(err)
			}
			allErrors = multierror.Append(allErrors, odherrors.NewStopError(status.COOMissingMessage))
		}
	}

	// Check for tempo-product operator if traces are enabled
	if monitoring.Spec.Traces != nil {
		if found, err := cluster.OperatorExists(ctx, rr.Client, tempoOperator); err != nil || !found {
			if err != nil {
				return odherrors.NewStopErrorW(err)
			}
			allErrors = multierror.Append(allErrors, odherrors.NewStopError(status.TempoOperatorMissingMessage))
		}
	}

	return allErrors.ErrorOrNil()
}

func addPrometheusRules(componentName string, rr *odhtypes.ReconciliationRequest) error {
	componentRules := fmt.Sprintf("%s/monitoring/%s-prometheusrules.tmpl.yaml", componentName, componentName)

	if !common.FileExists(componentMonitoring.ComponentRulesFS, componentRules) {
		return fmt.Errorf("prometheus rules file for component %s not found", componentName)
	}

	rr.Templates = append(rr.Templates, odhtypes.TemplateInfo{
		FS:   componentMonitoring.ComponentRulesFS,
		Path: componentRules,
	})

	return nil
}

// if a component is disabled, we need to delete the prometheus rules. If the DSCI is deleted
// the rules will be gc'd automatically.
func cleanupPrometheusRules(ctx context.Context, componentName string, rr *odhtypes.ReconciliationRequest) error {
	pr := &unstructured.Unstructured{}
	pr.SetGroupVersionKind(gvk.PrometheusRule)
	pr.SetName(fmt.Sprintf("%s-prometheusrules", componentName))
	pr.SetNamespace(rr.DSCI.Spec.Monitoring.Namespace)

	if err := rr.Client.Delete(ctx, pr); err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to delete prometheus rule for component %s: %w", componentName, err)
	}

	return nil
}

// addMetricsData adds metrics configuration data to the template data map.
func addMetricsData(ctx context.Context, rr *odhtypes.ReconciliationRequest, metrics *serviceApi.Metrics, templateData map[string]any) error {
	addResourceData(metrics, templateData)
	addStorageData(metrics, templateData)
	addReplicasData(ctx, rr, metrics, templateData)
	return addExportersData(metrics, templateData)
}

// addResourceData adds resource configuration data to the template data map.
func addResourceData(metrics *serviceApi.Metrics, templateData map[string]any) {
	if metrics.Resources != nil {
		templateData["CPULimit"] = getResourceValueOrDefault(metrics.Resources.CPULimit.String(), defaultCPULimit)
		templateData["MemoryLimit"] = getResourceValueOrDefault(metrics.Resources.MemoryLimit.String(), defaultMemoryLimit)
		templateData["CPURequest"] = getResourceValueOrDefault(metrics.Resources.CPURequest.String(), defaultCPURequest)
		templateData["MemoryRequest"] = getResourceValueOrDefault(metrics.Resources.MemoryRequest.String(), defaultMemoryRequest)
	} else {
		// Use defaults when Resources is nil
		templateData["CPULimit"] = defaultCPULimit
		templateData["MemoryLimit"] = defaultMemoryLimit
		templateData["CPURequest"] = defaultCPURequest
		templateData["MemoryRequest"] = defaultMemoryRequest
	}
}

// addStorageData adds storage configuration data to the template data map.
func addStorageData(metrics *serviceApi.Metrics, templateData map[string]any) {
	if metrics.Storage != nil {
		templateData["StorageSize"] = getResourceValueOrDefault(metrics.Storage.Size.String(), defaultStorageSize)
		templateData["StorageRetention"] = getStringValueOrDefault(metrics.Storage.Retention, defaultRetention)
	} else {
		// Use defaults when Storage is nil
		templateData["StorageSize"] = defaultStorageSize
		templateData["StorageRetention"] = defaultRetention
	}
}

// addReplicasData adds replica configuration data to the template data map.
func addReplicasData(ctx context.Context, rr *odhtypes.ReconciliationRequest, metrics *serviceApi.Metrics, templateData map[string]any) {
	// - if user explicitly set replicas, use their value
	// - if metrics is configured (storage or resources) but no explicit replicas, use SNO-aware defaults
	// - otherwise, rely on MonitoringStack CRD defaults
	allowedByConfig := metrics.Storage != nil || metrics.Resources != nil
	isSNO := isSingleNodeCluster(ctx, rr)

	switch {
	case metrics.Replicas != 0 && allowedByConfig:
		templateData["Replicas"] = strconv.Itoa(int(metrics.Replicas))
	case allowedByConfig:
		if isSNO {
			templateData["Replicas"] = "1"
		} else {
			templateData["Replicas"] = "2"
		}
	default:
		// Don't set replicas, let MonitoringStack CRD use its defaults
	}
}

// addExportersData adds custom metrics exporters data to the template data map.
func addExportersData(metrics *serviceApi.Metrics, templateData map[string]any) error {
	if metrics.Exporters == nil {
		return nil
	}

	validExporters := make(map[string]interface{})
	exporterNames := make([]string, 0, len(metrics.Exporters))

	for name, configYAML := range metrics.Exporters {
		if isReservedExporterName(name) {
			return fmt.Errorf("exporter name '%s' is reserved and cannot be used", name)
		}

		// Trim and parse YAML configuration string
		cfgStr := strings.TrimSpace(configYAML)
		var cfg interface{}
		if err := yaml.Unmarshal([]byte(cfgStr), &cfg); err != nil {
			return fmt.Errorf("invalid YAML configuration for exporter '%s': %w", name, err)
		}

		// Require a mapping/object to avoid invalid collector config
		cfgMap, ok := cfg.(map[string]interface{})
		if !ok {
			return fmt.Errorf("exporter '%s' configuration must be a YAML mapping/object", name)
		}

		validExporters[name] = cfgMap
		exporterNames = append(exporterNames, name)
	}

	// Ensure deterministic order in templates/pipelines
	sort.Strings(exporterNames)
	templateData["MetricsExporters"] = validExporters
	templateData["MetricsExporterNames"] = exporterNames

	return nil
}

// addTracesData adds traces configuration data to the template data map.
func addTracesData(traces *serviceApi.Traces, namespace string, templateData map[string]any) {
	templateData["OtlpEndpoint"] = fmt.Sprintf("http://data-science-collector.%s.svc.cluster.local:4317", namespace)
	templateData["SampleRatio"] = traces.SampleRatio
	templateData["Backend"] = traces.Storage.Backend // backend has default "pv" set in API

	tlsEnabled := determineTLSEnabled(traces)
	templateData["TempoTLSEnabled"] = tlsEnabled

	if tlsEnabled && traces.TLS != nil {
		templateData["TempoCertificateSecret"] = traces.TLS.CertificateSecret
		templateData["TempoCAConfigMap"] = traces.TLS.CAConfigMap
	} else {
		// Set empty values to avoid template missing key errors
		templateData["TempoCertificateSecret"] = ""
		templateData["TempoCAConfigMap"] = ""
	}

	templateData["TracesRetention"] = traces.Storage.Retention.Duration.String()

	setTempoEndpointAndStorageData(traces, namespace, templateData)
}

// determineTLSEnabled determines if TLS should be enabled for traces.
func determineTLSEnabled(traces *serviceApi.Traces) bool {
	if traces.TLS != nil {
		return traces.TLS.Enabled
	}
	return traces.Storage.Backend == "pv"
}

// setTempoEndpointAndStorageData sets the tempo endpoint and storage-specific data.
func setTempoEndpointAndStorageData(traces *serviceApi.Traces, namespace string, templateData map[string]any) {
	switch traces.Storage.Backend {
	case "pv":
		templateData["TempoEndpoint"] = fmt.Sprintf("tempo-data-science-tempomonolithic.%s.svc.cluster.local:4317", namespace)
		templateData["Size"] = traces.Storage.Size
	case "s3", "gcs":
		// Always use gateway endpoint for S3/GCS backends (required for OpenShift mode)
		templateData["TempoEndpoint"] = fmt.Sprintf("tempo-data-science-tempostack-gateway.%s.svc.cluster.local:4317", namespace)
		templateData["Secret"] = traces.Storage.Secret
	}
}

// getResourceValueOrDefault returns the resource value or a default if empty or zero.
func getResourceValueOrDefault(value, defaultValue string) string {
	if value == "" || value == "0" {
		return defaultValue
	}
	return value
}

// getStringValueOrDefault returns the string value or a default if empty.
func getStringValueOrDefault(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

// isReservedExporterName checks if an exporter name conflicts with built-in exporters.
func isReservedExporterName(name string) bool {
	switch name {
	case "prometheus", "otlp/tempo":
		return true
	default:
		return false
	}
}
