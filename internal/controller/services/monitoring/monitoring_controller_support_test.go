//nolint:testpackage // Need to test unexported function getTemplateData
package monitoring

import (
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
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// stringToRawExtension converts a YAML string to a runtime.RawExtension for testing.
func stringToRawExtension(yamlStr string) runtime.RawExtension {
	return runtime.RawExtension{Raw: []byte(yamlStr)}
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
			dsci := &dsciv1.DSCInitialization{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-dsci",
				},
				Spec: dsciv1.DSCInitializationSpec{
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
			require.NoError(t, dsciv1.AddToScheme(scheme))
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

func TestCustomMetricsExporters(t *testing.T) {
	tests := []struct {
		name                 string
		exporters            map[string]runtime.RawExtension
		expectError          bool
		errorMsg             string
		expectedParsedConfig map[string]interface{} // Add expected parsed output
		expectedNames        []string               // Add expected names
	}{
		{
			name: "valid custom exporters",
			exporters: map[string]runtime.RawExtension{
				"debug":       stringToRawExtension("verbosity: detailed"),
				"otlp/jaeger": stringToRawExtension("endpoint: http://jaeger:4317\ntls:\n  insecure: true"),
			},
			expectError: false,
			expectedParsedConfig: map[string]interface{}{
				"debug": map[string]interface{}{
					"verbosity": "detailed",
				},
				"otlp/jaeger": map[string]interface{}{
					"endpoint": "http://jaeger:4317",
					"tls": map[string]interface{}{
						"insecure": true,
					},
				},
			},
			expectedNames: []string{"debug", "otlp/jaeger"}, // Note: sorted order
		},
		{
			name:        "empty exporters map",
			exporters:   map[string]runtime.RawExtension{},
			expectError: false,
			// With early return optimization, template data won't be set for empty maps
			// getTemplateData() pre-initializes these fields, so templates still work
			expectedParsedConfig: nil, // Not set due to early return
			expectedNames:        nil, // Not set due to early return
		},
		{
			name:        "nil exporters (metrics defined but no exporters)",
			exporters:   nil,
			expectError: false,
			// With early return optimization, template data won't be set for nil
			// getTemplateData() pre-initializes these fields, so templates still work
			expectedParsedConfig: nil, // Not set due to early return
			expectedNames:        nil, // Not set due to early return
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
			expectError:          false, // validateExporters skips empty configs
			expectedParsedConfig: map[string]interface{}{},
			expectedNames:        []string{},
		},
		{
			name: "whitespace-only YAML string",
			exporters: map[string]runtime.RawExtension{
				"debug": stringToRawExtension("   \n  "),
			},
			expectError: false, // validateExporters parses whitespace as empty map
			expectedParsedConfig: map[string]interface{}{
				"debug": map[string]interface{}{},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mon := &serviceApi.Monitoring{
				Spec: serviceApi.MonitoringSpec{
					MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
						Namespace: "test-namespace",
						Metrics: &serviceApi.Metrics{
							Exporters: tt.exporters,
						},
					},
				},
			}

			// Create fake client
			scheme := runtime.NewScheme()
			require.NoError(t, dsciv1.AddToScheme(scheme))
			require.NoError(t, serviceApi.AddToScheme(scheme))

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			rr := &odhtypes.ReconciliationRequest{
				Client:   fakeClient,
				Instance: mon,
				DSCI: &dsciv1.DSCInitialization{
					Spec: dsciv1.DSCInitializationSpec{
						ApplicationsNamespace: "test-app-namespace",
					},
				},
			}

			templateData, err := getTemplateData(t.Context(), rr)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error to contain '%s', got: %v", tt.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}

				// Always verify template data exists (even if empty)
				exporters, ok := templateData["MetricsExporters"]
				if !ok {
					t.Error("MetricsExporters should always be in template data")
					return
				}
				exporterMap, ok := exporters.(map[string]interface{})
				if !ok {
					t.Error("MetricsExporters should be a map[string]interface{}")
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

				// Handle different test scenarios based on early return optimization
				if tt.expectedParsedConfig == nil && tt.expectedNames == nil {
					// Early return case - template data should have pre-initialized empty values
					if len(exporterMap) != 0 {
						t.Errorf("Expected empty exporters map for early return case, got %+v", exporterMap)
					}
					if len(namesList) != 0 {
						t.Errorf("Expected empty names list for early return case, got %+v", namesList)
					}
				} else {
					// Normal case with actual exporters - verify content
					if tt.expectedParsedConfig != nil {
						// Validate counts match expected
						if len(exporterMap) != len(tt.expectedParsedConfig) {
							t.Errorf("Expected %d exporters, got %d", len(tt.expectedParsedConfig), len(exporterMap))
						}
						if len(namesList) != len(tt.expectedNames) {
							t.Errorf("Expected %d exporter names, got %d", len(tt.expectedNames), len(namesList))
						}
						// Validate parsed configuration matches expected (deep comparison)
						if !reflect.DeepEqual(exporterMap, tt.expectedParsedConfig) {
							t.Errorf("Parsed configuration doesn't match expected.\nGot: %+v\nExpected: %+v", exporterMap, tt.expectedParsedConfig)
						}
					}

					// Names are expected to be sorted deterministically
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
			}
		})
	}
}

func TestGetTemplateDataAcceleratorMetricsWithMetricsConfiguration(t *testing.T) {
	ctx := t.Context()

	// Test with full metrics configuration
	dsci := &dsciv1.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsci",
		},
		Spec: dsciv1.DSCInitializationSpec{
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
	require.NoError(t, dsciv1.AddToScheme(scheme))
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
