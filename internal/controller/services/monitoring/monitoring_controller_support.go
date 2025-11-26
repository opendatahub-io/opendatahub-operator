package monitoring

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/go-multierror"
	"gopkg.in/yaml.v3"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	controllerutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	apicommon "github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	componentMonitoring "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	// Dependent operators names. match the one in the operatorcondition..
	opentelemetryOperator        = "opentelemetry-operator"
	clusterObservabilityOperator = "cluster-observability-operator"
	tempoOperator                = "tempo-operator"

	defaultStorageSize = "5Gi"
	defaultRetention   = "90d"

	defaultCPULimit      = "1"
	defaultMemoryLimit   = "512Mi"
	defaultCPURequest    = "100m"
	defaultMemoryRequest = "256Mi"

	defaultCollectorCPULimit      = "1"
	defaultCollectorMemoryLimit   = "256Mi"
	defaultCollectorCPURequest    = "100m"
	defaultCollectorMemoryRequest = "256Mi"

	defaultTempoCPULimit      = "1"
	defaultTempoMemoryLimit   = "256Mi"
	defaultTempoCPURequest    = "100m"
	defaultTempoMemoryRequest = "256Mi"
)

var componentIDRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(?:/[A-Za-z0-9][A-Za-z0-9_-]*)?$`)

// getPersesImage returns the Perses image from environment variable or default.
// For RHOAI deployments, this is set via the CSV (via RHOAI-Build-Config/bundle/additional-images-patch.yaml).
// For ODH deployments, this uses the default value below.
//
// Note: This image version must stay compatible with the Cluster Observability Operator (COO) version
// that we depend on. When upgrading COO, verify Perses image compatibility and update accordingly.
// The current image is compatible with COO 1.2.2.
func getPersesImage() string {
	if image := os.Getenv("RELATED_IMAGE_PERSES"); image != "" {
		return image
	}

	return "registry.redhat.io/cluster-observability-operator/perses-0-50-rhel9:1.2.2-1752686994"
}

// isLocalServiceEndpoint checks if an endpoint URL is for a local/in-cluster service.
// Returns true for localhost, loopback IPs, cluster-local services, and single-label service names.
func isLocalServiceEndpoint(endpoint string) bool {
	// Parse URL first to check hostname only (not path or other components)
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return false
	}

	host := strings.ToLower(u.Hostname()) // strips port and normalizes case
	if host == "" {
		return false
	}

	// Check for localhost and loopback IPs (on hostname only)
	if host == "localhost" || host == "::1" || strings.HasPrefix(host, "127.") {
		return true
	}

	// Check for cluster-local domain suffixes (only on hostname)
	if strings.HasSuffix(host, ".svc.cluster.local") || strings.HasSuffix(host, ".svc") {
		return true
	}

	// Single-label hostnames (no dots, no colons) are typically in-cluster services in the same namespace
	// e.g., "custom-backend", "prometheus"
	// Note: Must exclude IPv6 literals like [2001:4860:4860::8888] which contain colons
	if !strings.Contains(host, ".") && !strings.Contains(host, ":") {
		return true
	}

	return false
}

func isReservedName(n string) bool {
	reservedNames := map[string]bool{
		"otlp/tempo": true,
		"prometheus": true,
	}
	return reservedNames[n]
}

func validateExporters(exporters map[string]runtime.RawExtension) (map[string]string, error) {
	validatedExporters := make(map[string]string)

	// Validate total size of all exporters combined
	totalSize := 0
	for _, rawConfig := range exporters {
		var raw []byte
		switch {
		case len(rawConfig.Raw) > 0:
			raw = rawConfig.Raw
		case rawConfig.Object != nil:
			b, err := yaml.Marshal(rawConfig.Object)
			if err != nil {
				continue // Skip malformed configs, they'll be caught in detailed validation
			}
			raw = b
		}
		totalSize += len(raw)
	}
	if totalSize > maxTotalExporterSize {
		return nil, fmt.Errorf("total exporter config size exceeds maximum of %d bytes (actual: %d bytes)",
			maxTotalExporterSize, totalSize)
	}

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

		// Validate individual exporter size (10KB limit)
		if len(raw) > maxExporterSize {
			return nil, fmt.Errorf("exporter '%s' config exceeds maximum size of %d bytes (actual: %d bytes)",
				name, maxExporterSize, len(raw))
		}

		// Convert RawExtension to a map for validation and YAML conversion
		var config map[string]interface{}
		if err := yaml.Unmarshal(raw, &config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal exporter config for '%s': %w", name, err)
		}
		// Treat empty/whitespace and YAML null as empty object for consistent rendering.
		if config == nil {
			config = map[string]interface{}{}
		}

		// Enhanced security validations
		if err := validateExporterConfigSecurity(name, config); err != nil {
			return nil, err
		}

		// Schema validation for known exporter types
		if err := validateExporterSchema(name, config); err != nil {
			return nil, err
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
		// Perses datasource needs HTTP query endpoint (port 3200)
		templateData["TempoQueryEndpoint"] = fmt.Sprintf("http://tempo-data-science-tempomonolithic.%s.svc.cluster.local:3200", namespace)
		templateData["Size"] = traces.Storage.Size
	case "s3", "gcs":
		templateData["TempoEndpoint"] = fmt.Sprintf("tempo-data-science-tempostack-gateway.%s.svc.cluster.local:4317", namespace)
		// Perses datasource needs HTTP query endpoint via gateway (port 8080)
		templateData["TempoQueryEndpoint"] = fmt.Sprintf("http://tempo-data-science-tempostack-gateway.%s.svc.cluster.local:8080", namespace)
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

// Images can be overridden via environment variables, with defaults based on platform.
func addImageURLs(rr *odhtypes.ReconciliationRequest, templateData map[string]any) {
	templateData["KubeRBACProxyImage"] = getImageURL(
		"RELATED_IMAGE_OSE_KUBE_RBAC_PROXY_IMAGE",
		"quay.io/brancz/kube-rbac-proxy:v0.20.0",
		"registry.redhat.io/openshift4/ose-kube-rbac-proxy-rhel9:v4.17",
		rr.Release.Name,
	)
	templateData["PromLabelProxyImage"] = getImageURL(
		"RELATED_IMAGE_OSE_PROM_LABEL_PROXY_IMAGE",
		"quay.io/prometheuscommunity/prom-label-proxy:v0.12.1",
		"registry.redhat.io/openshift4/ose-prom-label-proxy-rhel9:v4.17",
		rr.Release.Name,
	)
	templateData["CLIImage"] = getImageURL(
		"RELATED_IMAGE_CLI_IMAGE",
		"quay.io/openshift/origin-cli:4.17",
		"registry.redhat.io/openshift4/ose-cli:v4.17",
		rr.Release.Name,
	)
}

func getImageURL(envVar, upstreamDefault, rhoaiDefault string, platform apicommon.Platform) string {
	if envValue := os.Getenv(envVar); envValue != "" {
		return envValue
	}

	if platform == cluster.ManagedRhoai || platform == cluster.SelfManagedRhoai {
		return rhoaiDefault
	}

	return upstreamDefault
}

func getTemplateData(ctx context.Context, rr *odhtypes.ReconciliationRequest) (map[string]any, error) {
	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return nil, errors.New("instance is not of type services.Monitoring")
	}

	// Fetch application namespace from DSCI.
	appNamespace, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return nil, err
	}

	// Fetch operator namespace
	operatorNamespace, err := cluster.GetOperatorNamespace()
	if err != nil {
		return nil, err
	}

	templateData := map[string]any{
		"Namespace":            monitoring.Spec.Namespace,
		"Traces":               monitoring.Spec.Traces != nil,
		"Metrics":              monitoring.Spec.Metrics != nil,
		"AcceleratorMetrics":   monitoring.Spec.Metrics != nil,
		"ApplicationNamespace": appNamespace,
		"OperatorNamespace":    operatorNamespace,
		"MetricsExporters":     make(map[string]string),
		"MetricsExporterNames": []string{},
		"PersesImage":          getPersesImage(),
	}

	// always add resource defaults
	addResourceData(templateData)
	addImageURLs(rr, templateData)

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

	templateData["CollectorReplicas"] = monitoring.Spec.CollectorReplicas

	return templateData, nil
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
	// Fetch monitoring namespace from DSCI
	monitoringNamespace, err := cluster.MonitoringNamespace(ctx, rr.Client)
	if err != nil {
		if k8serr.IsNotFound(err) {
			// No DSCI means no monitoring namespace configured, nothing to clean up
			return nil
		}
		return err
	}

	pr := &unstructured.Unstructured{}
	pr.SetGroupVersionKind(gvk.PrometheusRule)
	pr.SetName(fmt.Sprintf("%s-prometheusrules", componentName))
	pr.SetNamespace(monitoringNamespace)

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
	addStorageData(metrics, templateData)
	addReplicasData(ctx, rr, metrics, templateData)
	return addExportersData(metrics, templateData)
}

// addResourceData adds resource configuration data to the template data map.
func addResourceData(templateData map[string]any) {
	// Use defaults
	templateData["CPULimit"] = defaultCPULimit
	templateData["MemoryLimit"] = defaultMemoryLimit
	templateData["CPURequest"] = defaultCPURequest
	templateData["MemoryRequest"] = defaultMemoryRequest

	templateData["CollectorCPULimit"] = defaultCollectorCPULimit
	templateData["CollectorMemoryLimit"] = defaultCollectorMemoryLimit
	templateData["CollectorCPURequest"] = defaultCollectorCPURequest
	templateData["CollectorMemoryRequest"] = defaultCollectorMemoryRequest

	templateData["TempoCPULimit"] = defaultTempoCPULimit
	templateData["TempoMemoryLimit"] = defaultTempoMemoryLimit
	templateData["TempoCPURequest"] = defaultTempoCPURequest
	templateData["TempoMemoryRequest"] = defaultTempoMemoryRequest
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
	// - if metrics is configured but no explicit replicas, use SNO-aware defaults
	// - otherwise, rely on MonitoringStack CRD defaults
	allowedByConfig := metrics.Storage != nil
	isSNO := cluster.IsSingleNodeCluster(ctx, rr.Client)

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
	// Always initialize to avoid template rendering failures (consistent with traces)
	validatedExporters := make(map[string]string)
	exporterNames := make([]string, 0)

	// Early return if no exporters are configured
	if len(metrics.Exporters) == 0 {
		templateData["MetricsExporters"] = validatedExporters
		templateData["MetricsExporterNames"] = exporterNames
		return nil
	}

	// Validate exporters using the same function as traces
	var err error
	validatedExporters, err = validateExporters(metrics.Exporters)
	if err != nil {
		return err
	}

	// Build exporter names list for deterministic ordering (consistent with traces)
	for name := range validatedExporters {
		exporterNames = append(exporterNames, name)
	}
	sort.Strings(exporterNames)

	// Always set template data, even if empty, to prevent template rendering failures
	templateData["MetricsExporters"] = validatedExporters
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

const (
	// Security limits for exporter configurations.
	maxConfigFields      = 50    // Maximum number of fields in an exporter config.
	maxNestingDepth      = 10    // Maximum nesting depth to prevent deeply nested objects.
	maxStringLength      = 1024  // Maximum length for string values.
	maxArrayLength       = 100   // Maximum length for array values.
	maxExporterSize      = 10240 // Maximum size per exporter config (10KB).
	maxTotalExporterSize = 51200 // Maximum total size for all exporters combined (50KB).
)

// ExporterSchema defines the validation schema for an exporter type.
type ExporterSchema struct {
	RequiredFields []string
	AllowedFields  []string
	FieldTypes     map[string]FieldType
	FieldRules     map[string][]ValidationRule
}

// FieldType defines the expected type and constraints for a field.
type FieldType struct {
	Type          string
	Pattern       *regexp.Regexp
	MinLength     *int
	MaxLength     *int
	AllowedValues []string
}

// ValidationRule defines custom validation logic.
type ValidationRule struct {
	Name     string
	Validate func(field string, value interface{}) error
}

// Schema definitions for metrics exporters.
var metricsExporterSchemas = map[string]ExporterSchema{
	"otlp": {
		RequiredFields: []string{"endpoint"},
		AllowedFields: []string{
			"endpoint", "headers", "tls", "compression", "timeout",
			"retry_on_failure", "sending_queue", "balancer_name",
		},
		FieldTypes: map[string]FieldType{
			"endpoint": {
				Type:      "string",
				Pattern:   regexp.MustCompile(`^https?://[a-zA-Z0-9.-]+(:[0-9]+)?(/.*)?$`),
				MinLength: intPtr(1),
				MaxLength: intPtr(2048),
			},
			"headers": {
				Type: "map[string]string",
			},
			"tls": {
				Type: "object",
			},
			"compression": {
				Type:          "string",
				AllowedValues: []string{"gzip", "snappy", "zstd", "none"},
			},
			"timeout": {
				Type:      "string",
				Pattern:   regexp.MustCompile(`^\d+[smh]$`),
				MaxLength: intPtr(10),
			},
		},
		FieldRules: map[string][]ValidationRule{
			"endpoint": {
				{
					Name: "secure_endpoint_check",
					Validate: func(field string, value interface{}) error {
						if str, ok := value.(string); ok {
							if strings.HasPrefix(str, "http://") && !isLocalServiceEndpoint(str) {
								return errors.New("insecure HTTP endpoints not allowed for external services")
							}
						}
						return nil
					},
				},
			},
		},
	},
	"otlphttp": {
		RequiredFields: []string{"endpoint"},
		AllowedFields: []string{
			"endpoint", "headers", "tls", "compression", "timeout",
			"retry_on_failure", "sending_queue",
		},
		FieldTypes: map[string]FieldType{
			"endpoint": {
				Type:      "string",
				Pattern:   regexp.MustCompile(`^https?://[a-zA-Z0-9.-]+(:[0-9]+)?(/.*)?$`),
				MinLength: intPtr(1),
				MaxLength: intPtr(2048),
			},
			"headers": {
				Type: "map[string]string",
			},
			"compression": {
				Type:          "string",
				AllowedValues: []string{"gzip", "none"},
			},
			"timeout": {
				Type:      "string",
				Pattern:   regexp.MustCompile(`^\d+[smh]$`),
				MaxLength: intPtr(10),
			},
		},
		FieldRules: map[string][]ValidationRule{
			"endpoint": {
				{
					Name: "secure_endpoint_check",
					Validate: func(field string, value interface{}) error {
						if str, ok := value.(string); ok {
							if strings.HasPrefix(str, "http://") && !isLocalServiceEndpoint(str) {
								return errors.New("insecure HTTP endpoints not allowed for external services")
							}
						}
						return nil
					},
				},
			},
		},
	},
	"debug": {
		AllowedFields: []string{"verbosity", "sampling_initial", "sampling_thereafter"},
		FieldTypes: map[string]FieldType{
			"verbosity": {
				Type:          "string",
				AllowedValues: []string{"basic", "normal", "detailed"},
			},
			"sampling_initial": {
				Type: "int",
			},
			"sampling_thereafter": {
				Type: "int",
			},
		},
	},
	"prometheusremotewrite": {
		RequiredFields: []string{"endpoint"},
		AllowedFields: []string{
			"endpoint", "headers", "tls", "remote_timeout", "retry_on_failure",
			"sending_queue", "write_relabel_configs", "resource_to_telemetry_conversion",
		},
		FieldTypes: map[string]FieldType{
			"endpoint": {
				Type:      "string",
				Pattern:   regexp.MustCompile(`^https?://[a-zA-Z0-9.-]+(:[0-9]+)?(/.*)?$`),
				MinLength: intPtr(1),
				MaxLength: intPtr(2048),
			},
			"headers": {
				Type: "map[string]string",
			},
			"tls": {
				Type: "object",
			},
			"remote_timeout": {
				Type:      "string",
				Pattern:   regexp.MustCompile(`^\d+[smh]$`),
				MaxLength: intPtr(10),
			},
		},
		FieldRules: map[string][]ValidationRule{
			"endpoint": {
				{
					Name: "secure_endpoint_check",
					Validate: func(field string, value interface{}) error {
						if str, ok := value.(string); ok {
							if strings.HasPrefix(str, "http://") && !isLocalServiceEndpoint(str) {
								return errors.New("insecure HTTP endpoints not allowed for external services")
							}
						}
						return nil
					},
				},
			},
		},
	},
}

// validateExporterConfigSecurity performs additional security validations on exporter configurations.
func validateExporterConfigSecurity(name string, config map[string]interface{}) error {
	// Check maximum number of fields
	if len(config) > maxConfigFields {
		return fmt.Errorf("exporter '%s' has too many fields (%d), maximum allowed is %d", name, len(config), maxConfigFields)
	}

	// Check nesting depth and validate types recursively
	if err := validateConfigDepthAndTypes(config, 1, name); err != nil {
		return err
	}

	return nil
}

// validateConfigDepthAndTypes recursively validates the depth and types of configuration values.
func validateConfigDepthAndTypes(obj interface{}, depth int, exporterName string) error {
	if depth > maxNestingDepth {
		return fmt.Errorf("exporter '%s' config nesting too deep (max %d levels)", exporterName, maxNestingDepth)
	}

	switch v := obj.(type) {
	case map[string]interface{}:
		if len(v) > maxConfigFields {
			return fmt.Errorf("exporter '%s' config object has too many fields at depth %d", exporterName, depth)
		}
		for key, value := range v {
			// Validate key length
			if len(key) > maxStringLength {
				return fmt.Errorf("exporter '%s' config key too long at depth %d", exporterName, depth)
			}
			// Recursively validate nested values
			if err := validateConfigDepthAndTypes(value, depth+1, exporterName); err != nil {
				return err
			}
		}
	case []interface{}:
		if len(v) > maxArrayLength {
			return fmt.Errorf("exporter '%s' config array too long (%d items) at depth %d", exporterName, len(v), depth)
		}
		for _, item := range v {
			if err := validateConfigDepthAndTypes(item, depth+1, exporterName); err != nil {
				return err
			}
		}
	case string:
		if len(v) > maxStringLength {
			return fmt.Errorf("exporter '%s' config string value too long at depth %d", exporterName, depth)
		}
	case int, int32, int64, float32, float64, bool:
		// These types are safe
	case nil:
		// Nil values are safe
	default:
		return fmt.Errorf("exporter '%s' config contains unsupported type %T at depth %d", exporterName, v, depth)
	}

	return nil
}

// validateExporterSchema validates an exporter config against its schema.
func validateExporterSchema(exporterName string, config map[string]interface{}) error {
	exporterType := getExporterType(exporterName)
	schema, exists := metricsExporterSchemas[exporterType]

	if !exists {
		// For unknown exporters, schema validation is skipped
		// Security validation already applied above
		return nil
	}

	return schema.Validate(exporterName, config)
}

// getExporterType extracts the base exporter type from a name like "otlp/custom".
func getExporterType(exporterName string) string {
	if idx := strings.Index(exporterName, "/"); idx != -1 {
		return exporterName[:idx]
	}
	return exporterName
}

// Validate validates an exporter config against the schema.
func (s ExporterSchema) Validate(exporterName string, config map[string]interface{}) error {
	// Check required fields
	for _, required := range s.RequiredFields {
		if _, exists := config[required]; !exists {
			return fmt.Errorf("exporter '%s' missing required field: %s", exporterName, required)
		}
	}

	// Check for disallowed fields
	for field := range config {
		if !contains(s.AllowedFields, field) {
			return fmt.Errorf("exporter '%s' contains disallowed field: %s (allowed: %v)",
				exporterName, field, s.AllowedFields)
		}
	}

	// Validate field types and constraints
	for field, value := range config {
		if fieldType, exists := s.FieldTypes[field]; exists {
			if err := validateFieldTypeAndConstraints(exporterName, field, value, fieldType); err != nil {
				return err
			}
		}

		// Apply custom validation rules
		if rules, exists := s.FieldRules[field]; exists {
			for _, rule := range rules {
				if err := rule.Validate(field, value); err != nil {
					return fmt.Errorf("exporter '%s' field '%s' failed rule '%s': %w",
						exporterName, field, rule.Name, err)
				}
			}
		}
	}

	return nil
}

// validateFieldTypeAndConstraints validates field type and applies constraints.
func validateFieldTypeAndConstraints(exporterName, field string, value interface{}, fieldType FieldType) error {
	// Type validation
	if err := validateFieldTypeStrict(field, value, fieldType.Type); err != nil {
		return fmt.Errorf("exporter '%s' field '%s': %w", exporterName, field, err)
	}

	// String-specific constraints
	if str, ok := value.(string); ok && fieldType.Type == "string" {
		if fieldType.MinLength != nil && len(str) < *fieldType.MinLength {
			return fmt.Errorf("exporter '%s' field '%s': minimum length %d, got %d",
				exporterName, field, *fieldType.MinLength, len(str))
		}
		if fieldType.MaxLength != nil && len(str) > *fieldType.MaxLength {
			return fmt.Errorf("exporter '%s' field '%s': maximum length %d, got %d",
				exporterName, field, *fieldType.MaxLength, len(str))
		}
		if fieldType.Pattern != nil && !fieldType.Pattern.MatchString(str) {
			return fmt.Errorf("exporter '%s' field '%s': does not match required pattern",
				exporterName, field)
		}
		if len(fieldType.AllowedValues) > 0 && !contains(fieldType.AllowedValues, str) {
			return fmt.Errorf("exporter '%s' field '%s': must be one of %v, got '%s'",
				exporterName, field, fieldType.AllowedValues, str)
		}
	}

	return nil
}

// validateFieldTypeStrict validates field types with enhanced error messages.
func validateFieldTypeStrict(_ string, value interface{}, expectedType string) error {
	switch expectedType {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string, got %T", value)
		}
	case "int":
		switch value.(type) {
		case int, int32, int64, float64: // JSON unmarshals numbers as float64
			// Valid numeric types
		default:
			return fmt.Errorf("expected int, got %T", value)
		}
	case "bool":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected bool, got %T", value)
		}
	case "object":
		if _, ok := value.(map[string]interface{}); !ok {
			return fmt.Errorf("expected object, got %T", value)
		}
	case "array":
		if _, ok := value.([]interface{}); !ok {
			return fmt.Errorf("expected array, got %T", value)
		}
	case "map[string]string":
		if m, ok := value.(map[string]interface{}); ok {
			for _, v := range m {
				if _, ok := v.(string); !ok {
					return fmt.Errorf("map value must be string, got %T", v)
				}
			}
		} else {
			return fmt.Errorf("expected map[string]string, got %T", value)
		}
	default:
		return fmt.Errorf("unsupported field type: %s", expectedType)
	}
	return nil
}

// Helper functions.
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func intPtr(i int) *int {
	return &i
}

// syncPrometheusWebTLSCA watches the prometheus-web-tls-ca ConfigMap and syncs its CA to a Secret.
// This action is a workaround until COO-1270 is complete, which will allow MonitoringStack
// to consume CA directly from ConfigMap. The service-ca operator injects the CA into the ConfigMap,
// but MonitoringStack requires it in a Secret. This action ensures the Secret stays in sync
// with the ConfigMap, especially important when the service-ca operator rotates certificates.
//
// Related JIRA: https://issues.redhat.com/browse/COO-1270
func syncPrometheusWebTLSCA(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	log := logf.FromContext(ctx).WithName("syncPrometheusWebTLSCA")

	monitoring, ok := rr.Instance.(*serviceApi.Monitoring)
	if !ok {
		return errors.New("instance is not of type *services.Monitoring")
	}

	if monitoring.Spec.Metrics == nil {
		return nil
	}

	namespace := monitoring.Spec.Namespace

	var configMap unstructured.Unstructured
	configMap.SetAPIVersion("v1")
	configMap.SetKind("ConfigMap")

	err := rr.Client.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      "prometheus-web-tls-ca",
	}, &configMap)

	if err != nil {
		if k8serr.IsNotFound(err) {
			log.V(1).Info("ConfigMap not found yet, will sync when created",
				"namespace", namespace, "name", "prometheus-web-tls-ca")
			return nil
		}
		return fmt.Errorf("failed to get CA ConfigMap: %w", err)
	}

	data, found, err := unstructured.NestedStringMap(configMap.Object, "data")
	if err != nil {
		return fmt.Errorf("failed to extract data from ConfigMap: %w", err)
	}
	if !found {
		log.V(1).Info("ConfigMap data field not found, service-ca operator may not have injected CA yet")
		return nil
	}

	caCert, found := data["service-ca.crt"]
	if !found || caCert == "" {
		log.V(1).Info("service-ca.crt not found in ConfigMap, service-ca operator may not have injected CA yet")
		return nil
	}

	secret := &unstructured.Unstructured{}
	secret.SetAPIVersion("v1")
	secret.SetKind("Secret")
	secret.SetNamespace(namespace)
	secret.SetName("prometheus-web-tls-ca")
	secret.SetLabels(map[string]string{
		"platform.opendatahub.io/part-of": "monitoring",
	})

	// Set TypeMeta explicitly for server-side apply
	secret.Object["apiVersion"] = "v1"
	secret.Object["kind"] = "Secret"

	// Set owner reference to Monitoring CR for garbage collection
	if err := controllerutil.SetControllerReference(monitoring, secret, rr.Client.Scheme()); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	if err := unstructured.SetNestedField(secret.Object, "Opaque", "type"); err != nil {
		return fmt.Errorf("failed to set secret type: %w", err)
	}

	secretData := map[string]interface{}{
		"service-ca.crt": caCert,
	}
	if err := unstructured.SetNestedMap(secret.Object, secretData, "stringData"); err != nil {
		return fmt.Errorf("failed to set secret data: %w", err)
	}

	// Apply the secret using server-side apply (create or update)
	opts := []client.PatchOption{
		client.ForceOwnership,
		client.FieldOwner(resources.PlatformFieldOwner),
	}
	if err := resources.Apply(ctx, rr.Client, secret, opts...); err != nil {
		return fmt.Errorf("failed to apply CA Secret: %w", err)
	}

	log.Info("Successfully synced CA from ConfigMap to Secret",
		"namespace", namespace,
		"configmap", "prometheus-web-tls-ca",
		"secret", "prometheus-web-tls-ca")

	return nil
}
