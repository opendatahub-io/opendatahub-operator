//nolint:testpackage // Need to test unexported function getTemplateData
package monitoring

import (
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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	testScheme "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
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

			// Create fake client
			scheme := runtime.NewScheme()
			require.NoError(t, dsciv2.AddToScheme(scheme))
			require.NoError(t, serviceApi.AddToScheme(scheme))

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			rr := &odhtypes.ReconciliationRequest{
				Client:   fakeClient,
				Instance: mon,
				DSCI: &dsciv2.DSCInitialization{
					Spec: dsciv2.DSCInitializationSpec{
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

func validateConditions(t *testing.T, rr *odhtypes.ReconciliationRequest, tt monitoringIntegrationTestCase) {
	t.Helper()
	msCondition := rr.Conditions.GetCondition(status.ConditionMonitoringStackAvailable)
	assert.NotNil(t, msCondition, "MonitoringStack condition should always be set")
	assert.Equal(t, tt.expectedMSConditionStatus, string(msCondition.Status),
		"MonitoringStack condition status should match expected")

	thanosCondition := rr.Conditions.GetCondition(status.ConditionThanosQuerierAvailable)
	assert.NotNil(t, thanosCondition,
		"ThanosQuerier condition should always be set (atomic deployment with MonitoringStack)")
	assert.Equal(t, tt.expectedTQConditionStatus, string(thanosCondition.Status),
		"ThanosQuerier condition status should match expected")
}

func validateTemplates(t *testing.T, rr *odhtypes.ReconciliationRequest, tt monitoringIntegrationTestCase, initialTemplateCount int) {
	t.Helper()
	finalTemplateCount := len(rr.Templates)
	expectedTotalTemplates := initialTemplateCount + tt.expectedMSTemplates + tt.expectedTQTemplates
	assert.Equal(t, expectedTotalTemplates, finalTemplateCount,
		"Expected %d total templates (%d initial + %d MS + %d TQ), got %d",
		expectedTotalTemplates, initialTemplateCount, tt.expectedMSTemplates, tt.expectedTQTemplates, finalTemplateCount)

	templatePaths := make([]string, 0, len(rr.Templates))
	for _, template := range rr.Templates {
		templatePaths = append(templatePaths, template.Path)
	}

	if tt.expectedMSTemplates > 0 {
		assert.Contains(t, templatePaths, MonitoringStackTemplate,
			"MonitoringStack template should be added when MS condition is True")
		assert.Contains(t, templatePaths, MonitoringStackAlertmanagerRBACTemplate,
			"Alertmanager RBAC template should be added when MS condition is True")
		assert.Contains(t, templatePaths, PrometheusRouteTemplate,
			"PrometheusRoute template should be added when MS condition is True")
	} else {
		assert.NotContains(t, templatePaths, MonitoringStackTemplate,
			"MonitoringStack template should not be added when MS condition is not True")
		assert.NotContains(t, templatePaths, MonitoringStackAlertmanagerRBACTemplate,
			"Alertmanager RBAC template should not be added when MS condition is not True")
		assert.NotContains(t, templatePaths, PrometheusRouteTemplate,
			"PrometheusRoute template should not be added when MS condition is not True")
	}

	if tt.expectedTQTemplates > 0 {
		assert.Contains(t, templatePaths, ThanosQuerierTemplate,
			"ThanosQuerier template should be added when TQ condition is True")
		assert.Contains(t, templatePaths, ThanosQuerierRouteTemplate,
			"ThanosQuerierRoute template should be added when TQ condition is True")
	} else {
		assert.NotContains(t, templatePaths, ThanosQuerierTemplate,
			"ThanosQuerier template should not be added when TQ condition is not True")
		assert.NotContains(t, templatePaths, ThanosQuerierRouteTemplate,
			"ThanosQuerierRoute template should not be added when TQ condition is not True")
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
			expectedMSTemplates:       3, // MonitoringStack + Alertmanager RBAC + PrometheusRoute
			expectedTQTemplates:       2, // ThanosQuerier + ThanosQuerierRoute
			description:               "When both CRDs are available and metrics configured, both should be deployed",
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
			objects := setupTestObjects(tt)

			fakeClient, err := setupFakeClient(objects, tt)
			require.NoError(t, err, "Failed to create fake client")

			monitoring, ok := objects[0].(*serviceApi.Monitoring)
			require.True(t, ok, "First object should be monitoring instance")

			rr := &odhtypes.ReconciliationRequest{
				Client:     fakeClient,
				Instance:   monitoring,
				Templates:  []odhtypes.TemplateInfo{},
				Conditions: conditions.NewManager(monitoring, status.ConditionTypeReady),
			}

			initialTemplateCount := len(rr.Templates)

			err = deployMonitoringStackWithQuerier(ctx, rr)
			require.NoError(t, err, "deployMonitoringStackWithQuerier should not return error")

			validateConditions(t, rr, tt)
			validateTemplates(t, rr, tt, initialTemplateCount)
		})
	}
}
