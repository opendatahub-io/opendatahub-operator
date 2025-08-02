//nolint:testpackage // Test accesses unexported function getTemplateData
package monitoring

import (
	"context"
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

func TestGetTemplateDataGPUMetrics(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name               string
		managementState    operatorv1.ManagementState
		metricsConfig      *serviceApi.Metrics
		expectedGPUMetrics bool
	}{
		{
			name:               "GPU metrics enabled - managed state with metrics config",
			managementState:    operatorv1.Managed,
			metricsConfig:      &serviceApi.Metrics{},
			expectedGPUMetrics: true,
		},
		{
			name:               "GPU metrics disabled - managed state without metrics config",
			managementState:    operatorv1.Managed,
			metricsConfig:      nil,
			expectedGPUMetrics: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test objects
			dsci := &dsciv1.DSCInitialization{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-dsci",
				},
				Spec: dsciv1.DSCInitializationSpec{
					Monitoring: serviceApi.DSCIMonitoring{
						ManagementSpec: common.ManagementSpec{
							ManagementState: tt.managementState,
						},
					},
				},
			}

			monitoring := &serviceApi.Monitoring{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-monitoring",
				},
				Spec: serviceApi.MonitoringSpec{
					MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
						Namespace: "test-namespace",
						Metrics:   tt.metricsConfig,
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

			// Verify GPU metrics configuration
			gpuMetrics, exists := templateData["GPUMetrics"]
			require.True(t, exists, "GPUMetrics key should exist in template data")
			assert.Equal(t, tt.expectedGPUMetrics, gpuMetrics, "GPUMetrics value should match expected")

			// Verify other expected fields still exist
			assert.Contains(t, templateData, "Namespace")
			assert.Contains(t, templateData, "Metrics")
			assert.Contains(t, templateData, "Traces")
		})
	}
}

func TestGetTemplateDataGPUMetricsWithMetricsConfiguration(t *testing.T) {
	ctx := context.Background()

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
			Name: "test-monitoring",
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

	// Verify GPU metrics is enabled with full metrics config
	gpuMetrics, exists := templateData["GPUMetrics"]
	require.True(t, exists)
	gpuMetricsBool, ok := gpuMetrics.(bool)
	require.True(t, ok, "GPUMetrics should be a boolean")
	assert.True(t, gpuMetricsBool, "GPUMetrics should be enabled with managed state and metrics config")

	// Verify metrics-related template data is populated
	metricsValue, exists := templateData["Metrics"]
	require.True(t, exists)
	metricsBool, ok := metricsValue.(bool)
	require.True(t, ok, "Metrics should be a boolean")
	assert.True(t, metricsBool)
	assert.Contains(t, templateData, "Replicas")
	assert.Contains(t, templateData, "StorageRetention")
}
