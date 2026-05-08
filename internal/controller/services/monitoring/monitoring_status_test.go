package monitoring_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/dscinitialization"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
)

func TestGetMonitoringReadyCondition(t *testing.T) {
	// Setup
	scheme := runtime.NewScheme()
	_ = serviceApi.AddToScheme(scheme)
	_ = dsciv2.AddToScheme(scheme)

	tests := []struct {
		name               string
		monitoringCR       *serviceApi.Monitoring
		expectedConditions []dscinitialization.DSCInitializationCondition
	}{
		{
			name:         "Monitoring CR not found",
			monitoringCR: nil,
			expectedConditions: []dscinitialization.DSCInitializationCondition{
				{
					Type:         status.ConditionMonitoringReady,
					ReadyReason:  status.RemovedReason,
					ReadyMessage: "Monitoring is not enabled",
					ReadyStatus:  metav1.ConditionFalse,
				},
			},
		},
		{
			name: "Monitoring CR exists but has no relevant conditions",
			monitoringCR: &serviceApi.Monitoring{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default-monitoring",
				},
				Status: serviceApi.MonitoringStatus{
					Status: common.Status{
						Conditions: []common.Condition{
							{
								Type:    "UnrelatedCondition",
								Status:  metav1.ConditionTrue,
								Reason:  "Ready",
								Message: "Some unrelated message",
							},
						},
					},
				},
			},
			expectedConditions: []dscinitialization.DSCInitializationCondition{
				{
					Type:         status.ConditionMonitoringReady,
					ReadyReason:  status.NotReadyReason,
					ReadyMessage: "Monitoring stack is initializing",
					ReadyStatus:  metav1.ConditionUnknown,
				},
			},
		},
		{
			name: "Monitoring CR has relevant conditions and all are True",
			monitoringCR: &serviceApi.Monitoring{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default-monitoring",
				},
				Status: serviceApi.MonitoringStatus{
					Status: common.Status{
						Conditions: []common.Condition{
							{
								Type:    status.ConditionTypeReady,
								Status:  metav1.ConditionTrue,
								Reason:  status.ReadyReason,
								Message: "Ready message",
							},
							{
								Type:    status.ConditionThanosQuerierAvailable,
								Status:  metav1.ConditionTrue,
								Reason:  status.ReadyReason,
								Message: "Thanos message",
							},
						},
					},
				},
			},
			expectedConditions: []dscinitialization.DSCInitializationCondition{
				{
					Type:         status.ConditionTypeReady,
					ReadyStatus:  metav1.ConditionTrue,
					ReadyReason:  status.ReadyReason,
					ReadyMessage: "Ready message",
				},
				{
					Type:         status.ConditionThanosQuerierAvailable,
					ReadyStatus:  metav1.ConditionTrue,
					ReadyReason:  status.ReadyReason,
					ReadyMessage: "Thanos message",
				},
				{
					Type:         status.ConditionMonitoringReady,
					ReadyStatus:  metav1.ConditionTrue,
					ReadyReason:  status.ReadyReason,
					ReadyMessage: "Monitoring stack is initialized",
				},
			},
		},
		{
			name: "Monitoring CR has a relevant condition that is False",
			monitoringCR: &serviceApi.Monitoring{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default-monitoring",
				},
				Status: serviceApi.MonitoringStatus{
					Status: common.Status{
						Conditions: []common.Condition{
							{
								Type:    status.ConditionTypeReady,
								Status:  metav1.ConditionTrue,
								Reason:  status.ReadyReason,
								Message: "Ready message",
							},
							{
								Type:    status.ConditionThanosQuerierAvailable,
								Status:  metav1.ConditionFalse,
								Reason:  "Degraded",
								Message: "Thanos is failing",
							},
						},
					},
				},
			},
			expectedConditions: []dscinitialization.DSCInitializationCondition{
				{
					Type:         status.ConditionTypeReady,
					ReadyStatus:  metav1.ConditionTrue,
					ReadyReason:  status.ReadyReason,
					ReadyMessage: "Ready message",
				},
				{
					Type:         status.ConditionThanosQuerierAvailable,
					ReadyStatus:  metav1.ConditionFalse,
					ReadyReason:  "Degraded",
					ReadyMessage: "Thanos is failing",
				},
				{
					Type:         status.ConditionMonitoringReady,
					ReadyStatus:  metav1.ConditionTrue,
					ReadyReason:  status.ReadyReason,
					ReadyMessage: "Monitoring stack is initialized",
				},
			},
		},
		{
			name: "Monitoring CR has a relevant condition that is Unknown",
			monitoringCR: &serviceApi.Monitoring{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default-monitoring",
				},
				Status: serviceApi.MonitoringStatus{
					Status: common.Status{
						Conditions: []common.Condition{
							{
								Type:    status.ConditionTypeReady,
								Status:  metav1.ConditionUnknown,
								Reason:  "Initializing",
								Message: "Wait for it",
							},
						},
					},
				},
			},
			expectedConditions: []dscinitialization.DSCInitializationCondition{
				{
					Type:         status.ConditionTypeReady,
					ReadyStatus:  metav1.ConditionUnknown,
					ReadyReason:  "Initializing",
					ReadyMessage: "Wait for it",
				},
				{
					Type:         status.ConditionMonitoringReady,
					ReadyStatus:  metav1.ConditionTrue,
					ReadyReason:  status.ReadyReason,
					ReadyMessage: "Monitoring stack is initialized",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			clientBuilder := fake.NewClientBuilder().WithScheme(scheme)
			if tt.monitoringCR != nil {
				clientBuilder.WithObjects(tt.monitoringCR)
			}
			r := &dscinitialization.DSCInitializationReconciler{
				Client: clientBuilder.Build(),
			}

			// Act
			conditions := r.GetMonitoringReadyCondition(context.Background())

			// Assert
			assert.Equal(t, tt.expectedConditions, conditions)
		})
	}
}
