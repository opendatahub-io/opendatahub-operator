//nolint:testpackage // Need to test unexported function getTemplateData
package monitoring

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	testScheme "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

func TestMain(m *testing.M) {
	// Set environment variables for operator namespace and platform type
	// This is required because cluster.GetOperatorNamespace() checks a cached value
	// that is set once during cluster.Init()
	os.Setenv("OPERATOR_NAMESPACE", "test-operator-ns")

	// Set platform type to avoid CatalogSource lookup during cluster.Init()
	os.Setenv("ODH_PLATFORM_TYPE", "OpenDataHub")

	// Initialize cluster config with a minimal fake client
	// This populates the package-level clusterConfig variable with the operator namespace
	scheme := runtime.NewScheme()
	_ = dsciv2.AddToScheme(scheme)
	_ = serviceApi.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Ignore errors from Init as we only care about setting the operator namespace
	// Other initialization errors (like missing cluster resources) are expected in tests
	_ = cluster.Init(context.Background(), fakeClient)

	os.Exit(m.Run())
}

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

// setupTestClient creates a fake client with the required scheme.
func setupTestClient(g Gomega, objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	g.Expect(dsciv2.AddToScheme(scheme)).Should(Succeed())
	g.Expect(serviceApi.AddToScheme(scheme)).Should(Succeed())

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()
}

func TestGetTemplateDataAcceleratorMetrics(t *testing.T) {
	ctx := t.Context()

	// Set environment variable for operator namespace (required by cluster.GetOperatorNamespace)
	t.Setenv("OPERATOR_NAMESPACE", "test-operator-ns")

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
			g := NewWithT(t)

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
			fakeClient := setupTestClient(g, dsci, monitoring)

			// Create reconciliation request
			rr := &odhtypes.ReconciliationRequest{
				Client:   fakeClient,
				Instance: monitoring,
			}

			// Test getTemplateData function
			templateData, err := getTemplateData(ctx, rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify accelerator metrics result
			acceleratorMetrics, exists := templateData["AcceleratorMetrics"]
			g.Expect(exists).Should(BeTrue())

			acceleratorMetricsBool, ok := acceleratorMetrics.(bool)
			g.Expect(ok).Should(BeTrue(), "AcceleratorMetrics should be a boolean")

			g.Expect(acceleratorMetricsBool).Should(Equal(tt.expectedAccelerator))
		})
	}
}

// runMetricsExporterTest creates a test environment and runs getTemplateData.
func runMetricsExporterTest(t *testing.T, exporters map[string]runtime.RawExtension) (map[string]interface{}, error) {
	t.Helper()
	g := NewWithT(t)

	// Set environment variable for operator namespace (required by cluster.GetOperatorNamespace)
	t.Setenv("OPERATOR_NAMESPACE", "test-operator-ns")

	// Create DSCI
	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsci",
		},
		Spec: dsciv2.DSCInitializationSpec{
			ApplicationsNamespace: "test-app-namespace",
		},
	}

	// Create Monitoring object
	monitoring := &serviceApi.Monitoring{
		Spec: serviceApi.MonitoringSpec{
			MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
				Namespace: "test-namespace",
				Metrics: &serviceApi.Metrics{
					Exporters: exporters,
				},
			},
		},
	}

	// Create fake client
	fakeClient := setupTestClient(g, dsci, monitoring)

	rr := &odhtypes.ReconciliationRequest{
		Client:   fakeClient,
		Instance: monitoring,
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
	// Set environment variable for operator namespace (required by cluster.GetOperatorNamespace)
	t.Setenv("OPERATOR_NAMESPACE", "test-operator-ns")

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
	g := NewWithT(t)

	// Set environment variable for operator namespace (required by cluster.GetOperatorNamespace)
	t.Setenv("OPERATOR_NAMESPACE", "test-operator-ns")

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
				},
			},
		},
	}

	// Create fake client
	fakeClient := setupTestClient(g, dsci, monitoring)

	// Create reconciliation request
	rr := &odhtypes.ReconciliationRequest{
		Client:   fakeClient,
		Instance: monitoring,
	}

	// Test getTemplateData function
	templateData, err := getTemplateData(ctx, rr)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify accelerator metrics is enabled with full metrics config
	acceleratorMetrics, exists := templateData["AcceleratorMetrics"]
	g.Expect(exists).Should(BeTrue())
	acceleratorMetricsBool, ok := acceleratorMetrics.(bool)
	g.Expect(ok).Should(BeTrue(), "AcceleratorMetrics should be a boolean")
	g.Expect(acceleratorMetricsBool).Should(BeTrue(), "AcceleratorMetrics should be enabled with managed state and metrics config")

	// Verify metrics-related template data is populated
	metricsValue, exists := templateData["Metrics"]
	g.Expect(exists).Should(BeTrue())
	metricsBool, ok := metricsValue.(bool)
	g.Expect(ok).Should(BeTrue(), "Metrics should be a boolean")
	g.Expect(metricsBool).Should(BeTrue())
	g.Expect(templateData).Should(HaveKey("Replicas"))
	g.Expect(templateData).Should(HaveKey("StorageRetention"))
}

type monitoringIntegrationTestCase struct {
	name                      string
	hasMetricsConfig          bool
	hasMonitoringStackCRD     bool
	hasThanosQuerierCRD       bool
	expectedMSConditionStatus string
	expectedTQConditionStatus string
	expectedMSTemplates       int
	expectedTQTemplates       int
	description               string
}

func createMonitoringStackCRD() *extv1.CustomResourceDefinition {
	return &extv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "monitoringstacks.monitoring.rhobs",
		},
		Spec: extv1.CustomResourceDefinitionSpec{
			Group: "monitoring.rhobs",
			Versions: []extv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Schema: &extv1.CustomResourceValidation{
						OpenAPIV3Schema: &extv1.JSONSchemaProps{
							Type: "object",
						},
					},
				},
			},
			Scope: extv1.NamespaceScoped,
			Names: extv1.CustomResourceDefinitionNames{
				Plural: "monitoringstacks",
				Kind:   "MonitoringStack",
			},
		},
		Status: extv1.CustomResourceDefinitionStatus{
			StoredVersions: []string{"v1alpha1"},
			Conditions: []extv1.CustomResourceDefinitionCondition{
				{
					Type:   extv1.Established,
					Status: extv1.ConditionTrue,
				},
			},
		},
	}
}

func createThanosQuerierCRD() *extv1.CustomResourceDefinition {
	return &extv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "thanosqueriers.monitoring.rhobs",
		},
		Spec: extv1.CustomResourceDefinitionSpec{
			Group: "monitoring.rhobs",
			Versions: []extv1.CustomResourceDefinitionVersion{
				{
					Name:    "v1alpha1",
					Served:  true,
					Storage: true,
					Schema: &extv1.CustomResourceValidation{
						OpenAPIV3Schema: &extv1.JSONSchemaProps{
							Type: "object",
						},
					},
				},
			},
			Scope: extv1.NamespaceScoped,
			Names: extv1.CustomResourceDefinitionNames{
				Plural: "thanosqueriers",
				Kind:   "ThanosQuerier",
			},
		},
		Status: extv1.CustomResourceDefinitionStatus{
			StoredVersions: []string{"v1alpha1"},
			Conditions: []extv1.CustomResourceDefinitionCondition{
				{
					Type:   extv1.Established,
					Status: extv1.ConditionTrue,
				},
			},
		},
	}
}

func setupTestObjects(tt monitoringIntegrationTestCase) []client.Object {
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

	if tt.hasMetricsConfig {
		monitoring.Spec.Metrics = &serviceApi.Metrics{
			Replicas: 1,
		}
	}

	objects := []client.Object{monitoring}

	if tt.hasMonitoringStackCRD {
		objects = append(objects, createMonitoringStackCRD())
	}

	if tt.hasThanosQuerierCRD {
		objects = append(objects, createThanosQuerierCRD())
	}

	return objects
}

func setupFakeClient(objects []client.Object, tt monitoringIntegrationTestCase) (client.Client, error) {
	scheme, err := testScheme.New()
	if err != nil {
		return nil, err
	}

	fakeMapper := meta.NewDefaultRESTMapper(scheme.PreferredVersionAllGroups())

	for kt := range scheme.AllKnownTypes() {
		switch kt {
		// k8s
		case gvk.CustomResourceDefinition:
			fakeMapper.Add(kt, meta.RESTScopeRoot)
		case gvk.ClusterRole:
			fakeMapper.Add(kt, meta.RESTScopeRoot)
		// ODH
		case gvk.DataScienceCluster:
			fakeMapper.Add(kt, meta.RESTScopeRoot)
		case gvk.DSCInitialization:
			fakeMapper.Add(kt, meta.RESTScopeRoot)
		default:
			fakeMapper.Add(kt, meta.RESTScopeNamespace)
		}
	}

	// Add external CRDs to the RESTMapper
	if tt.hasMonitoringStackCRD {
		fakeMapper.Add(gvk.MonitoringStack, meta.RESTScopeNamespace)
	}
	if tt.hasThanosQuerierCRD {
		fakeMapper.Add(gvk.ThanosQuerier, meta.RESTScopeNamespace)
	}

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithRESTMapper(fakeMapper).
		WithObjects(objects...).
		Build(), nil
}

func validateConditions(t *testing.T, g Gomega, rr *odhtypes.ReconciliationRequest, tt monitoringIntegrationTestCase) {
	t.Helper()
	msCondition := rr.Conditions.GetCondition(status.ConditionMonitoringStackAvailable)
	g.Expect(msCondition).ShouldNot(BeNil(), "MonitoringStack condition should always be set")
	g.Expect(string(msCondition.Status)).Should(Equal(tt.expectedMSConditionStatus),
		"MonitoringStack condition status should match")

	thanosCondition := rr.Conditions.GetCondition(status.ConditionThanosQuerierAvailable)
	g.Expect(thanosCondition).ShouldNot(BeNil(),
		"ThanosQuerier condition should always be set (atomic deployment with MonitoringStack)")
	g.Expect(string(thanosCondition.Status)).Should(Equal(tt.expectedTQConditionStatus),
		"ThanosQuerier condition status should match expected")
}

func validateTemplates(t *testing.T, g Gomega, rr *odhtypes.ReconciliationRequest, tt monitoringIntegrationTestCase, initialTemplateCount int) {
	t.Helper()
	finalTemplateCount := len(rr.Templates)
	expectedTotalTemplates := initialTemplateCount + tt.expectedMSTemplates + tt.expectedTQTemplates
	g.Expect(finalTemplateCount).Should(Equal(expectedTotalTemplates),
		"Total template count should match expected value")

	templatePaths := make([]string, 0, len(rr.Templates))
	for _, template := range rr.Templates {
		templatePaths = append(templatePaths, template.Path)
	}

	if tt.expectedMSTemplates > 0 {
		g.Expect(templatePaths).Should(ContainElement(MonitoringStackTemplate),
			"MonitoringStack template should be included when enabled")
		g.Expect(templatePaths).Should(ContainElement(MonitoringStackAlertmanagerRBACTemplate),
			"Alertmanager RBAC template should be included when MonitoringStack enabled")
		g.Expect(templatePaths).Should(ContainElement(PrometheusRouteTemplate),
			"Prometheus route template should be included when MonitoringStack enabled")
	} else {
		g.Expect(templatePaths).ShouldNot(ContainElement(MonitoringStackTemplate),
			"MonitoringStack template should be excluded when disabled")
		g.Expect(templatePaths).ShouldNot(ContainElement(MonitoringStackAlertmanagerRBACTemplate),
			"Alertmanager RBAC template should be excluded when MonitoringStack disabled")
		g.Expect(templatePaths).ShouldNot(ContainElement(PrometheusRouteTemplate),
			"Prometheus route template should be excluded when MonitoringStack disabled")
	}

	if tt.expectedTQTemplates > 0 {
		g.Expect(templatePaths).Should(ContainElement(ThanosQuerierTemplate),
			"ThanosQuerier template should be included when enabled")
		g.Expect(templatePaths).Should(ContainElement(ThanosQuerierRouteTemplate),
			"ThanosQuerier route template should be included when enabled")
	} else {
		g.Expect(templatePaths).ShouldNot(ContainElement(ThanosQuerierTemplate),
			"ThanosQuerier template should be excluded when disabled")
		g.Expect(templatePaths).ShouldNot(ContainElement(ThanosQuerierRouteTemplate),
			"ThanosQuerier route template should be excluded when disabled")
	}
}

func TestMonitoringStackThanosQuerierIntegration(t *testing.T) {
	ctx := t.Context()

	tests := []monitoringIntegrationTestCase{
		{
			name:                      "Both CRDs available with metrics - both deployed",
			hasMetricsConfig:          true,
			hasMonitoringStackCRD:     true,
			hasThanosQuerierCRD:       true,
			expectedMSConditionStatus: "True",
			expectedTQConditionStatus: "True",
			expectedMSTemplates:       8, // MonitoringStack + Alertmanager RBAC + PrometheusRoute +
			// PrometheusServiceOverride + PrometheusNetworkPolicy + PrometheusWebTLSService +
			// PrometheusNamespaceProxy + PrometheusNamespaceProxyNetworkPolicy
			expectedTQTemplates: 2, // ThanosQuerier + ThanosQuerierRoute
			description:         "When both CRDs are available and metrics configured, both should be deployed",
		},
		{
			name:                      "Only MonitoringStack CRD available with metrics - both conditions false, atomic deployment",
			hasMetricsConfig:          true,
			hasMonitoringStackCRD:     true,
			hasThanosQuerierCRD:       false,
			expectedMSConditionStatus: "False",
			expectedTQConditionStatus: "False",
			expectedMSTemplates:       0,
			expectedTQTemplates:       0,
			description:               "When only MonitoringStack CRD is available, neither should be deployed (atomic deployment)",
		},
		{
			name:                      "Only ThanosQuerier CRD available with metrics - both conditions false, atomic deployment",
			hasMetricsConfig:          true,
			hasMonitoringStackCRD:     false,
			hasThanosQuerierCRD:       true,
			expectedMSConditionStatus: "False",
			expectedTQConditionStatus: "False",
			expectedMSTemplates:       0,
			expectedTQTemplates:       0,
			description:               "When only ThanosQuerier CRD is available, neither should be deployed (atomic deployment)",
		},
		{
			name:                      "No CRDs available with metrics - both conditions false, no templates",
			hasMetricsConfig:          true,
			hasMonitoringStackCRD:     false,
			hasThanosQuerierCRD:       false,
			expectedMSConditionStatus: "False",
			expectedTQConditionStatus: "False",
			expectedMSTemplates:       0,
			expectedTQTemplates:       0,
			description:               "When no CRDs are available, conditions should be false but no errors",
		},
		{
			name:                      "No metrics configuration - both conditions false",
			hasMetricsConfig:          false,
			hasMonitoringStackCRD:     true,
			hasThanosQuerierCRD:       true,
			expectedMSConditionStatus: "False",
			expectedTQConditionStatus: "False",
			expectedMSTemplates:       0,
			expectedTQTemplates:       0,
			description:               "When no metrics configured, neither MonitoringStack nor ThanosQuerier should be deployed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			objects := setupTestObjects(tt)

			fakeClient, err := setupFakeClient(objects, tt)
			g.Expect(err).ShouldNot(HaveOccurred(), "Failed to create fake client")

			monitoring, ok := objects[0].(*serviceApi.Monitoring)
			g.Expect(ok).Should(BeTrue(), "First object should be monitoring instance")

			rr := &odhtypes.ReconciliationRequest{
				Client:     fakeClient,
				Instance:   monitoring,
				Templates:  []odhtypes.TemplateInfo{},
				Conditions: conditions.NewManager(monitoring, status.ConditionTypeReady),
			}

			initialTemplateCount := len(rr.Templates)

			err = deployMonitoringStackWithQuerierAndRestrictions(ctx, rr)
			require.NoError(t, err, "deployMonitoringStackWithQuerierAndRestrictions should not return error")

			validateConditions(t, g, rr, tt)
			validateTemplates(t, g, rr, tt, initialTemplateCount)
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

func TestGetImageURL(t *testing.T) {
	tests := []struct {
		name            string
		envVar          string
		envValue        string
		upstreamDefault string
		rhoaiDefault    string
		platform        common.Platform
		expected        string
	}{
		{
			name:            "Environment variable set",
			envVar:          "TEST_IMAGE_URL",
			envValue:        "custom.registry.io/custom-image:v1.0",
			upstreamDefault: "upstream.io/image:v1.0",
			rhoaiDefault:    "redhat.io/image:v1.0",
			platform:        common.Platform("OpenShift AI Self-Managed"),
			expected:        "custom.registry.io/custom-image:v1.0",
		},
		{
			name:            "RHOAI Self-Managed without env var",
			envVar:          "TEST_IMAGE_URL",
			envValue:        "",
			upstreamDefault: "upstream.io/image:v1.0",
			rhoaiDefault:    "redhat.io/image:v1.0",
			platform:        common.Platform("OpenShift AI Self-Managed"),
			expected:        "redhat.io/image:v1.0",
		},
		{
			name:            "RHOAI Managed without env var",
			envVar:          "TEST_IMAGE_URL",
			envValue:        "",
			upstreamDefault: "upstream.io/image:v1.0",
			rhoaiDefault:    "redhat.io/image:v1.0",
			platform:        common.Platform("OpenShift AI Cloud Service"),
			expected:        "redhat.io/image:v1.0",
		},
		{
			name:            "OpenDataHub without env var",
			envVar:          "TEST_IMAGE_URL",
			envValue:        "",
			upstreamDefault: "upstream.io/image:v1.0",
			rhoaiDefault:    "redhat.io/image:v1.0",
			platform:        common.Platform("Open Data Hub"),
			expected:        "upstream.io/image:v1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv(tt.envVar, tt.envValue)
			}

			result := getImageURL(tt.envVar, tt.upstreamDefault, tt.rhoaiDefault, tt.platform)

			if result != tt.expected {
				t.Errorf("getImageURL() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetTemplateDataImageURLs(t *testing.T) {
	ctx := t.Context()

	// Set environment variable for operator namespace (required by cluster.GetOperatorNamespace)
	t.Setenv("OPERATOR_NAMESPACE", "test-operator-ns")

	tests := []struct {
		name                   string
		platform               common.Platform
		envKubeRBACProxy       string
		envPromLabelProxy      string
		expectedKubeRBACProxy  string
		expectedPromLabelProxy string
	}{
		{
			name:                   "OpenDataHub with no env vars",
			platform:               common.Platform("Open Data Hub"),
			envKubeRBACProxy:       "",
			envPromLabelProxy:      "",
			expectedKubeRBACProxy:  "quay.io/brancz/kube-rbac-proxy:v0.20.0",
			expectedPromLabelProxy: "quay.io/prometheuscommunity/prom-label-proxy:v0.12.1",
		},
		{
			name:                   "RHOAI Self-Managed with no env vars",
			platform:               common.Platform("OpenShift AI Self-Managed"),
			envKubeRBACProxy:       "",
			envPromLabelProxy:      "",
			expectedKubeRBACProxy:  "registry.redhat.io/openshift4/ose-kube-rbac-proxy-rhel9:v4.17",
			expectedPromLabelProxy: "registry.redhat.io/openshift4/ose-prom-label-proxy-rhel9:v4.17",
		},
		{
			name:                   "RHOAI Managed with no env vars",
			platform:               common.Platform("OpenShift AI Cloud Service"),
			envKubeRBACProxy:       "",
			envPromLabelProxy:      "",
			expectedKubeRBACProxy:  "registry.redhat.io/openshift4/ose-kube-rbac-proxy-rhel9:v4.17",
			expectedPromLabelProxy: "registry.redhat.io/openshift4/ose-prom-label-proxy-rhel9:v4.17",
		},
		{
			name:                   "Custom images via env vars",
			platform:               common.Platform("OpenShift AI Self-Managed"),
			envKubeRBACProxy:       "custom.io/kube-rbac-proxy:custom",
			envPromLabelProxy:      "custom.io/prom-label-proxy:custom",
			expectedKubeRBACProxy:  "custom.io/kube-rbac-proxy:custom",
			expectedPromLabelProxy: "custom.io/prom-label-proxy:custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable for operator namespace (required by cluster.GetOperatorNamespace)
			t.Setenv("OPERATOR_NAMESPACE", "test-operator-ns")

			if tt.envKubeRBACProxy != "" {
				t.Setenv("RELATED_IMAGE_OSE_KUBE_RBAC_PROXY_IMAGE", tt.envKubeRBACProxy)
			}
			if tt.envPromLabelProxy != "" {
				t.Setenv("RELATED_IMAGE_OSE_PROM_LABEL_PROXY_IMAGE", tt.envPromLabelProxy)
			}

			dsci := &dsciv2.DSCInitialization{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-dsci",
				},
				Spec: dsciv2.DSCInitializationSpec{
					ApplicationsNamespace: "test-apps",
				},
			}

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

			scheme := runtime.NewScheme()
			require.NoError(t, dsciv2.AddToScheme(scheme))
			require.NoError(t, serviceApi.AddToScheme(scheme))

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(dsci, monitoring).
				Build()

			rr := &odhtypes.ReconciliationRequest{
				Client:   fakeClient,
				Instance: monitoring,
				Release: common.Release{
					Name: tt.platform,
				},
			}

			templateData, err := getTemplateData(ctx, rr)
			require.NoError(t, err)

			// Verify image URLs are present and correct
			kubeRBACProxy, ok := templateData["KubeRBACProxyImage"]
			require.True(t, ok, "KubeRBACProxyImage should be present in template data")
			assert.Equal(t, tt.expectedKubeRBACProxy, kubeRBACProxy)

			promLabelProxy, ok := templateData["PromLabelProxyImage"]
			require.True(t, ok, "PromLabelProxyImage should be present in template data")
			assert.Equal(t, tt.expectedPromLabelProxy, promLabelProxy)
		})
	}
}
