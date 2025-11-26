//nolint:testpackage // Need to test unexported function deployNodeMetricsEndpoint
package monitoring

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func TestDeployNodeMetricsEndpoint(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name                    string
		hasMetricsConfig        bool
		expectedCondition       bool
		expectedTemplateCount   int
		expectedConditionReason string
	}{
		{
			name:                    "with metrics config",
			hasMetricsConfig:        true,
			expectedCondition:       true,
			expectedTemplateCount:   1, // PrometheusClusterProxyTemplate only
			expectedConditionReason: "",
		},
		{
			name:                    "without metrics config",
			hasMetricsConfig:        false,
			expectedCondition:       false,
			expectedTemplateCount:   0,
			expectedConditionReason: status.MetricsNotConfiguredReason,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create Monitoring object
			monitoring := &serviceApi.Monitoring{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default-monitoring",
					Namespace: "test-namespace",
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
				WithObjects(monitoring).
				Build()

			// Create reconciliation request
			rr := &odhtypes.ReconciliationRequest{
				Client:     fakeClient,
				Instance:   monitoring,
				Templates:  []odhtypes.TemplateInfo{},
				Conditions: conditions.NewManager(monitoring, status.ConditionTypeReady),
			}

			// Test deployNodeMetricsEndpoint function
			err := deployNodeMetricsEndpoint(ctx, rr)
			require.NoError(t, err)

			// Verify template count
			assert.Len(t, rr.Templates, tt.expectedTemplateCount)

			// Verify condition
			condition := conditions.FindStatusCondition(rr.Instance, status.ConditionNodeMetricsEndpointAvailable)
			require.NotNil(t, condition)

			if tt.expectedCondition {
				assert.Equal(t, metav1.ConditionTrue, condition.Status)
			} else {
				assert.Equal(t, metav1.ConditionFalse, condition.Status)
				assert.Equal(t, tt.expectedConditionReason, condition.Reason)
			}

			// If templates are expected, verify they contain the right paths
			if tt.expectedTemplateCount > 0 {
				templatePaths := make([]string, len(rr.Templates))
				for i, template := range rr.Templates {
					templatePaths[i] = template.Path
				}
				assert.Contains(t, templatePaths, PrometheusClusterProxyTemplate)
			}
		})
	}
}

func TestNodeMetricsEndpointTemplateConstants(t *testing.T) {
	// Verify that the template constants are properly defined
	assert.Equal(t, "resources/data-science-prometheus-cluster-proxy.tmpl.yaml", PrometheusClusterProxyTemplate)
}
