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
		exporters            map[string]string
		expectError          bool
		errorMsg             string
		expectedParsedConfig map[string]interface{} // Add expected parsed output
		expectedNames        []string               // Add expected names
	}{
		{
			name: "valid custom exporters",
			exporters: map[string]string{
				"logging":     "loglevel: debug",
				"otlp/jaeger": "endpoint: http://jaeger:4317\ntls:\n  insecure: true",
			},
			expectError: false,
			expectedParsedConfig: map[string]interface{}{
				"logging": map[string]interface{}{
					"loglevel": "debug",
				},
				"otlp/jaeger": map[string]interface{}{
					"endpoint": "http://jaeger:4317",
					"tls": map[string]interface{}{
						"insecure": true,
					},
				},
			},
			expectedNames: []string{"logging", "otlp/jaeger"}, // Note: sorted order
		},
		{
			name:                 "empty exporters map",
			exporters:            map[string]string{},
			expectError:          false,
			expectedParsedConfig: map[string]interface{}{},
			expectedNames:        []string{},
		},
		{
			name:                 "nil exporters (metrics defined but no exporters)",
			exporters:            nil,
			expectError:          false,
			expectedParsedConfig: map[string]interface{}{},
			expectedNames:        []string{},
		},
		{
			name: "reserved name prometheus",
			exporters: map[string]string{
				"prometheus": "endpoint: http://example.com",
			},
			expectError: true,
			errorMsg:    "reserved",
		},
		{
			name: "reserved name otlp/tempo",
			exporters: map[string]string{
				"otlp/tempo": "endpoint: http://tempo.example.com",
			},
			expectError: true,
			errorMsg:    "reserved",
		},
		{
			name: "invalid YAML",
			exporters: map[string]string{
				"logging": "loglevel: [unclosed",
			},
			expectError: true,
			errorMsg:    "invalid YAML",
		},
		{
			name: "empty YAML string",
			exporters: map[string]string{
				"logging": "",
			},
			expectError: true,
			errorMsg:    "must be a YAML mapping/object",
		},
		{
			name: "whitespace-only YAML string",
			exporters: map[string]string{
				"logging": "   \n  ",
			},
			expectError: true,
			errorMsg:    "must be a YAML mapping/object",
		},
		{
			name: "scalar YAML (not object)",
			exporters: map[string]string{
				"logging": "debug", // This is a scalar, not an object
			},
			expectError: true,
			errorMsg:    "must be a YAML mapping/object",
		},
		{
			name: "list YAML (not object)",
			exporters: map[string]string{
				"logging": "- endpoint: http://example:4317",
			},
			expectError: true,
			errorMsg:    "must be a YAML mapping/object",
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

			rr := &odhtypes.ReconciliationRequest{
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

				// For cases with actual exporters, verify content
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
