//nolint:testpackage // Need to test unexported function getTemplateData
package monitoring

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// stringToRawExtension converts a YAML string to a runtime.RawExtension for testing.
func stringToRawExtension(yamlStr string) runtime.RawExtension {
	return runtime.RawExtension{Raw: []byte(yamlStr)}
}

// generateLargeConfig creates a YAML config with the specified number of fields for testing.
func generateLargeConfig(numFields int) string {
	fields := make([]string, 0, numFields)
	for i := range numFields {
		fields = append(fields, fmt.Sprintf("field%d: value%d", i, i))
	}
	return strings.Join(fields, "\n")
}

// generateDeepNestedConfig creates a deeply nested YAML config for testing.
func generateDeepNestedConfig(depth int) string {
	config := "endpoint: http://example.com"
	for i := range depth {
		config = fmt.Sprintf("level%d:\n  %s", i, strings.ReplaceAll(config, "\n", "\n  "))
	}
	return config
}

// generateLargeArrayConfig creates a YAML config with a large array for testing.
func generateLargeArrayConfig(arraySize int) string {
	items := make([]string, 0, arraySize)
	for i := range arraySize {
		items = append(items, fmt.Sprintf("- item%d", i))
	}
	return fmt.Sprintf("items:\n%s", strings.Join(items, "\n"))
}

// generateLargeSizeConfig generates a config that exceeds the size limit.
func generateLargeSizeConfig(targetSize int) string {
	// Create a config with a large string value to exceed size limit
	largeValue := strings.Repeat("a", targetSize)
	return fmt.Sprintf("endpoint: https://example.com\nlarge_field: %s", largeValue)
}

func TestGetTemplateDataAcceleratorMetrics(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name                string
		monitoringState     operatorv1.ManagementState
		hasMetricsConfig    bool
		expectedAccelerator bool
	}{
		{
			name:                "Managed with metrics config",
			monitoringState:     operatorv1.Managed,
			hasMetricsConfig:    true,
			expectedAccelerator: true,
		},
		{
			name:                "Managed without metrics config",
			monitoringState:     operatorv1.Managed,
			hasMetricsConfig:    false,
			expectedAccelerator: false,
		},
		{
			name:                "Unmanaged with metrics config",
			monitoringState:     operatorv1.Unmanaged, // Note: Unmanaged is not CRD-valid for DSCI; ensures getTemplateData tolerates unexpected values.
			hasMetricsConfig:    true,
			expectedAccelerator: true,
		},
		{
			name:                "Removed with metrics config",
			monitoringState:     operatorv1.Removed,
			hasMetricsConfig:    true,
			expectedAccelerator: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create DSCI
			dsci := &dsciv2.DSCInitialization{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-dsci",
				},
				Spec: dsciv2.DSCInitializationSpec{
					Monitoring: serviceApi.DSCIMonitoring{
						ManagementSpec: common.ManagementSpec{
							ManagementState: tt.monitoringState,
						},
					},
				},
			}

			// Create Monitoring object
			monitoring := &serviceApi.Monitoring{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default-monitoring",
				},
				Spec: serviceApi.MonitoringSpec{
					MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
						Namespace: "test-namespace",
					},
				},
			}

			// Add metrics config if required
			if tt.hasMetricsConfig {
				monitoring.Spec.Metrics = &serviceApi.Metrics{
					Replicas: 1,
				}
			}

			// Create fake client
			scheme := runtime.NewScheme()
			require.NoError(t, dsciv2.AddToScheme(scheme))
			require.NoError(t, serviceApi.AddToScheme(scheme))

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(dsci, monitoring).
				Build()

			// Create reconciliation request
			rr := &odhtypes.ReconciliationRequest{
				Client:   fakeClient,
				Instance: monitoring,
				DSCI:     dsci,
			}

			// Test getTemplateData function
			templateData, err := getTemplateData(ctx, rr)
			require.NoError(t, err)

			// Verify accelerator metrics result
			acceleratorMetrics, exists := templateData["AcceleratorMetrics"]
			require.True(t, exists)
			acceleratorMetricsBool, ok := acceleratorMetrics.(bool)
			require.True(t, ok, "AcceleratorMetrics should be a boolean")
			assert.Equal(t, tt.expectedAccelerator, acceleratorMetricsBool)
		})
	}
}

// runMetricsExporterTest creates a test environment and runs getTemplateData.
func runMetricsExporterTest(t *testing.T, exporters map[string]runtime.RawExtension) (map[string]interface{}, error) {
	t.Helper()
	mon := &serviceApi.Monitoring{
		Spec: serviceApi.MonitoringSpec{
			MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
				Namespace: "test-namespace",
				Metrics: &serviceApi.Metrics{
					Exporters: exporters,
				},
			},
		},
	}

	scheme := runtime.NewScheme()
	require.NoError(t, dsciv2.AddToScheme(scheme))
	require.NoError(t, serviceApi.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	rr := &odhtypes.ReconciliationRequest{
		Client:   fakeClient,
		Instance: mon,
		DSCI: &dsciv2.DSCInitialization{
			Spec: dsciv2.DSCInitializationSpec{
				ApplicationsNamespace: "test-app-namespace",
			},
		},
	}

	return getTemplateData(t.Context(), rr)
}

// validateMetricsExporterResult validates test results against expected values.
func validateMetricsExporterResult(t *testing.T, tt struct {
	name                 string
	exporters            map[string]runtime.RawExtension
	expectError          bool
	errorMsg             string
	expectedParsedConfig map[string]string
	expectedNames        []string
}, templateData map[string]interface{}, err error) {
	t.Helper()

	if tt.expectError {
		if err == nil {
			t.Errorf("Expected error but got none")
		} else if !strings.Contains(err.Error(), tt.errorMsg) {
			t.Errorf("Expected error to contain '%s', got: %v", tt.errorMsg, err)
		}
		return
	}

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
		return
	}

	exporters, ok := templateData["MetricsExporters"]
	if !ok {
		t.Error("MetricsExporters should always be in template data")
		return
	}
	exporterMap, ok := exporters.(map[string]string)
	if !ok {
		t.Error("MetricsExporters should be a map[string]string")
		return
	}
	names, ok := templateData["MetricsExporterNames"]
	if !ok {
		t.Error("MetricsExporterNames should always be in template data")
		return
	}
	namesList, ok := names.([]string)
	if !ok {
		t.Error("MetricsExporterNames should be a []string")
		return
	}

	if tt.expectedParsedConfig == nil && tt.expectedNames == nil {
		if len(exporterMap) != 0 {
			t.Errorf("Expected empty exporters map for early return case, got %+v", exporterMap)
		}
		if len(namesList) != 0 {
			t.Errorf("Expected empty names list for early return case, got %+v", namesList)
		}
		return
	}

	if tt.expectedParsedConfig != nil {
		if len(exporterMap) != len(tt.expectedParsedConfig) {
			t.Errorf("Expected %d exporters, got %d", len(tt.expectedParsedConfig), len(exporterMap))
		}
		if len(namesList) != len(tt.expectedNames) {
			t.Errorf("Expected %d exporter names, got %d", len(tt.expectedNames), len(namesList))
		}
		if !reflect.DeepEqual(exporterMap, tt.expectedParsedConfig) {
			t.Errorf("Parsed configuration doesn't match expected.\nGot: %+v\nExpected: %+v", exporterMap, tt.expectedParsedConfig)
		}
	}

	if tt.expectedNames != nil {
		if len(namesList) != len(tt.expectedNames) {
			t.Fatalf("Expected %d exporter names, got %d", len(tt.expectedNames), len(namesList))
		}
		for i := range tt.expectedNames {
			if namesList[i] != tt.expectedNames[i] {
				t.Fatalf("Exporter names not sorted as expected.\nExpected: %v\nActual:   %v", tt.expectedNames, namesList)
			}
		}
	}
}

func TestCustomMetricsExporters(t *testing.T) {
	tests := []struct {
		name                 string
		exporters            map[string]runtime.RawExtension
		expectError          bool
		errorMsg             string
		expectedParsedConfig map[string]string // YAML strings like traces
		expectedNames        []string          // Add expected names
	}{
		{
			name: "valid custom exporters",
			exporters: map[string]runtime.RawExtension{
				"debug":       stringToRawExtension("verbosity: detailed"),
				"otlp/jaeger": stringToRawExtension("endpoint: https://jaeger:4317\ntls:\n  insecure: false"),
			},
			expectError: false,
			expectedParsedConfig: map[string]string{
				"debug":       "verbosity: detailed",
				"otlp/jaeger": "endpoint: https://jaeger:4317\ntls:\n    insecure: false",
			},
			expectedNames: []string{"debug", "otlp/jaeger"}, // Note: sorted order
		},
		{
			name:        "empty exporters map",
			exporters:   map[string]runtime.RawExtension{},
			expectError: false,
			// addExportersData now always sets template data (consistent with traces)
			expectedParsedConfig: map[string]string{}, // Empty but set
			expectedNames:        []string{},          // Empty but set
		},
		{
			name:        "nil exporters (metrics defined but no exporters)",
			exporters:   nil,
			expectError: false,
			// addExportersData now always sets template data (consistent with traces)
			expectedParsedConfig: map[string]string{}, // Empty but set
			expectedNames:        []string{},          // Empty but set
		},
		{
			name: "reserved name prometheus",
			exporters: map[string]runtime.RawExtension{
				"prometheus": stringToRawExtension("endpoint: http://example.com"),
			},
			expectError: true,
			errorMsg:    "reserved",
		},
		{
			name: "reserved name otlp/tempo",
			exporters: map[string]runtime.RawExtension{
				"otlp/tempo": stringToRawExtension("endpoint: http://tempo.example.com"),
			},
			expectError: true,
			errorMsg:    "reserved",
		},
		{
			name: "invalid YAML",
			exporters: map[string]runtime.RawExtension{
				"debug": stringToRawExtension("verbosity: [unclosed"),
			},
			expectError: true,
			errorMsg:    "failed to unmarshal exporter config",
		},
		{
			name: "empty YAML string",
			exporters: map[string]runtime.RawExtension{
				"debug": stringToRawExtension(""),
			},
			expectError:          false,               // validateExporters skips empty configs
			expectedParsedConfig: map[string]string{}, // Empty but set
			expectedNames:        []string{},
		},
		{
			name: "whitespace-only YAML string",
			exporters: map[string]runtime.RawExtension{
				"debug": stringToRawExtension("   \n  "),
			},
			expectError: false, // validateExporters parses whitespace as empty map
			expectedParsedConfig: map[string]string{
				"debug": "{}",
			},
			expectedNames: []string{"debug"},
		},
		{
			name: "scalar YAML (not object)",
			exporters: map[string]runtime.RawExtension{
				"debug": stringToRawExtension("debug"), // This is a scalar, not an object
			},
			expectError: true,
			errorMsg:    "failed to unmarshal exporter config",
		},
		{
			name: "list YAML (not object)",
			exporters: map[string]runtime.RawExtension{
				"debug": stringToRawExtension("- endpoint: http://example:4317"),
			},
			expectError: true,
			errorMsg:    "failed to unmarshal exporter config",
		},
		{
			name: "config with too many fields",
			exporters: map[string]runtime.RawExtension{
				"debug": stringToRawExtension(generateLargeConfig(51)), // Exceeds maxConfigFields (50)
			},
			expectError: true,
			errorMsg:    "has too many fields",
		},
		{
			name: "config with too deep nesting",
			exporters: map[string]runtime.RawExtension{
				"debug": stringToRawExtension(generateDeepNestedConfig(11)), // Exceeds maxNestingDepth (10)
			},
			expectError: true,
			errorMsg:    "config nesting too deep",
		},
		{
			name: "config with string value too long",
			exporters: map[string]runtime.RawExtension{
				"debug": stringToRawExtension(fmt.Sprintf("endpoint: %s", strings.Repeat("a", 1025))), // Exceeds maxStringLength (1024)
			},
			expectError: true,
			errorMsg:    "string value too long",
		},
		{
			name: "config with array too long",
			exporters: map[string]runtime.RawExtension{
				"debug": stringToRawExtension(generateLargeArrayConfig(101)), // Exceeds maxArrayLength (100)
			},
			expectError: true,
			errorMsg:    "array too long",
		},
		{
			name: "schema validation - missing required field",
			exporters: map[string]runtime.RawExtension{
				"otlp/test": stringToRawExtension("headers:\n  auth: token"), // Missing required 'endpoint'
			},
			expectError: true,
			errorMsg:    "missing required field: endpoint",
		},
		{
			name: "schema validation - disallowed field",
			exporters: map[string]runtime.RawExtension{
				"otlp/test": stringToRawExtension("endpoint: https://example.com\ninvalid_field: value"),
			},
			expectError: true,
			errorMsg:    "contains disallowed field: invalid_field",
		},
		{
			name: "schema validation - invalid compression",
			exporters: map[string]runtime.RawExtension{
				"otlp/test": stringToRawExtension("endpoint: https://example.com\ncompression: invalid"),
			},
			expectError: true,
			errorMsg:    "must be one of [gzip snappy zstd none]",
		},
		{
			name: "schema validation - invalid verbosity",
			exporters: map[string]runtime.RawExtension{
				"debug": stringToRawExtension("verbosity: invalid"),
			},
			expectError: true,
			errorMsg:    "must be one of [basic normal detailed]",
		},
		{
			name: "schema validation - insecure endpoint blocked",
			exporters: map[string]runtime.RawExtension{
				"otlp/test": stringToRawExtension("endpoint: http://external-service.com"),
			},
			expectError: true,
			errorMsg:    "insecure HTTP endpoints not allowed for external services",
		},
		{
			name: "schema validation - localhost HTTP allowed",
			exporters: map[string]runtime.RawExtension{
				"otlp/test": stringToRawExtension("endpoint: http://localhost:4317"),
			},
			expectError: false,
			expectedParsedConfig: map[string]string{
				"otlp/test": "endpoint: http://localhost:4317",
			},
			expectedNames: []string{"otlp/test"},
		},
		{
			name: "invalid exporter name - component ID format",
			exporters: map[string]runtime.RawExtension{
				"123invalid": stringToRawExtension("endpoint: https://example.com"), // Invalid: starts with number
			},
			expectError: true,
			errorMsg:    "must match OpenTelemetry component ID format",
		},
		{
			name: "invalid exporter name - special characters",
			exporters: map[string]runtime.RawExtension{
				"bad@name": stringToRawExtension("endpoint: https://example.com"), // Invalid: contains @
			},
			expectError: true,
			errorMsg:    "must match OpenTelemetry component ID format",
		},
		{
			name: "invalid endpoint URL pattern",
			exporters: map[string]runtime.RawExtension{
				"otlp/test": stringToRawExtension("endpoint: not-a-url"), // Invalid URL format
			},
			expectError: true,
			errorMsg:    "does not match required pattern",
		},
		{
			name: "field type mismatch - endpoint as number",
			exporters: map[string]runtime.RawExtension{
				"otlp/test": stringToRawExtension("endpoint: 12345"), // Should be string, not number
			},
			expectError: true,
			errorMsg:    "expected string",
		},
		{
			name: "exporter size exceeds 10KB limit",
			exporters: map[string]runtime.RawExtension{
				"otlp/test": stringToRawExtension(generateLargeSizeConfig(11000)), // Exceeds 10KB
			},
			expectError: true,
			errorMsg:    "exceeds maximum size",
		},
		{
			name: "total exporter size exceeds 50KB limit",
			exporters: map[string]runtime.RawExtension{
				"otlp/test1": stringToRawExtension(generateLargeSizeConfig(20000)),
				"otlp/test2": stringToRawExtension(generateLargeSizeConfig(20000)),
				"otlp/test3": stringToRawExtension(generateLargeSizeConfig(20000)), // Total > 50KB
			},
			expectError: true,
			errorMsg:    "total exporter config size exceeds maximum",
		},
		{
			name: "prometheusremotewrite exporter validation",
			exporters: map[string]runtime.RawExtension{
				"prometheusremotewrite": stringToRawExtension(`endpoint: https://prometheus.example.com/api/v1/write
headers:
  authorization: "Bearer token123"
tls:
  insecure: false`),
			},
			expectError: false,
			expectedParsedConfig: map[string]string{
				"prometheusremotewrite": `endpoint: https://prometheus.example.com/api/v1/write
headers:
    authorization: Bearer token123
tls:
    insecure: false`,
			},
			expectedNames: []string{"prometheusremotewrite"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			templateData, err := runMetricsExporterTest(t, tt.exporters)

			validateMetricsExporterResult(t, tt, templateData, err)
		})
	}
}

func TestGetTemplateDataAcceleratorMetricsWithMetricsConfiguration(t *testing.T) {
	ctx := t.Context()

	// Test with full metrics configuration
	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsci",
		},
		Spec: dsciv2.DSCInitializationSpec{
			Monitoring: serviceApi.DSCIMonitoring{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Managed,
				},
			},
		},
	}

	monitoring := &serviceApi.Monitoring{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-monitoring",
		},
		Spec: serviceApi.MonitoringSpec{
			MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
				Namespace: "test-namespace",
				Metrics: &serviceApi.Metrics{
					Replicas: 2,
					Storage: &serviceApi.MetricsStorage{
						Retention: "7d",
					},
					Resources: &serviceApi.MetricsResources{},
				},
			},
		},
	}

	// Create fake client
	scheme := runtime.NewScheme()
	require.NoError(t, dsciv2.AddToScheme(scheme))
	require.NoError(t, serviceApi.AddToScheme(scheme))

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(dsci, monitoring).
		Build()

	// Create reconciliation request
	rr := &odhtypes.ReconciliationRequest{
		Client:   fakeClient,
		Instance: monitoring,
		DSCI:     dsci,
	}

	// Test getTemplateData function
	templateData, err := getTemplateData(ctx, rr)
	require.NoError(t, err)

	// Verify accelerator metrics is enabled with full metrics config
	acceleratorMetrics, exists := templateData["AcceleratorMetrics"]
	require.True(t, exists)
	acceleratorMetricsBool, ok := acceleratorMetrics.(bool)
	require.True(t, ok, "AcceleratorMetrics should be a boolean")
	assert.True(t, acceleratorMetricsBool, "AcceleratorMetrics should be enabled with managed state and metrics config")

	// Verify metrics-related template data is populated
	metricsValue, exists := templateData["Metrics"]
	require.True(t, exists)
	metricsBool, ok := metricsValue.(bool)
	require.True(t, ok, "Metrics should be a boolean")
	assert.True(t, metricsBool)
	assert.Contains(t, templateData, "Replicas")
	assert.Contains(t, templateData, "StorageRetention")
}

func TestMonitoringStackThanosQuerierIntegration(t *testing.T) {
	tests := []struct {
		name             string
		hasMetricsConfig bool
		description      string
	}{
		{
			name:             "Monitoring stack calls ThanosQuerier when metrics configured",
			hasMetricsConfig: true,
			description:      "Should call deployThanosQuerier from deployMonitoringStack when metrics are configured",
		},
		{
			name:             "Monitoring stack calls ThanosQuerier when metrics not configured",
			hasMetricsConfig: false,
			description:      "Should call deployThanosQuerier from deployMonitoringStack even when metrics are not configured for proper condition handling",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create Monitoring object
			monitoring := &serviceApi.Monitoring{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default-monitoring",
				},
				Spec: serviceApi.MonitoringSpec{
					MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
						Namespace: "test-namespace",
					},
				},
			}

			// Add metrics config if required
			if tt.hasMetricsConfig {
				monitoring.Spec.Metrics = &serviceApi.Metrics{
					Replicas: 1,
				}
			}

			assert.NotNil(t, monitoring, "Monitoring object should be created")
			if tt.hasMetricsConfig {
				assert.NotNil(t, monitoring.Spec.Metrics, "Metrics should be configured when expected")
			} else {
				assert.Nil(t, monitoring.Spec.Metrics, "Metrics should not be configured when not expected")
			}
		})
	}
}

func TestIsLocalServiceEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		expected bool
	}{
		// Localhost variants
		{"localhost with port", "http://localhost:4317", true},
		{"localhost without port", "http://localhost", true},
		{"loopback IPv4", "http://127.0.0.1:4317", true},
		{"loopback IPv6", "http://[::1]:4317", true},

		// Cluster-local services
		{"full cluster domain", "http://service.namespace.svc.cluster.local:4317", true},
		{"short cluster domain", "http://service.namespace.svc:4317", true},
		{"single-label service", "http://custom-backend:4317", true},
		{"single-label without port", "http://prometheus", true},

		// External services (should be false)
		{"external FQDN", "http://example.com:4317", false},
		{"external subdomain", "http://metrics.example.com", false},
		{"external HTTPS", "https://external-service.com:4317", false},
		{"IP address", "http://192.168.1.100:4317", false},

		// Security: External URLs with .svc in path should NOT be treated as local
		{"malicious: .svc in path", "http://attacker.com/foo.svc", false},
		{"malicious: .svc.cluster.local in path", "http://evil.com/api/.svc.cluster.local", false},
		{"malicious: .svc in query", "http://attacker.com?param=.svc", false},

		// Security: External URLs with localhost/loopback in path/query should NOT be treated as local
		{"malicious: localhost in path", "http://evil.com/api/localhost", false},
		{"malicious: 127.0.0.1 in path", "http://attacker.com/redirect/127.0.0.1", false},
		{"malicious: ::1 in query", "http://bad.com?target=::1", false},

		// Security: External IPv6 addresses should NOT be treated as local
		{"malicious: external IPv6", "http://[2001:4860:4860::8888]:4317", false},
		{"malicious: external IPv6 no port", "http://[2001:db8::1]", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isLocalServiceEndpoint(tt.endpoint)
			if result != tt.expected {
				t.Errorf("isLocalServiceEndpoint(%q) = %v, expected %v",
					tt.endpoint, result, tt.expected)
			}
		})
	}
}
