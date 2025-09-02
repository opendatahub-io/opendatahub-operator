package v1alpha1

import (
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

// TestServiceTypesConformToPlatformObject validates that all service types properly implement
// the common.PlatformObject interface, which is required for integration with the
// operator framework. This test ensures:
//
// 1. Each service type can be cast to PlatformObject interface (compile-time verification)
// 2. GetStatus() method returns the correct status object
// 3. GetConditions() and SetConditions() work properly for status management
//
// This is critical because the operator framework relies on these interface methods
// for status reporting, condition management, and reconciliation logic.
//
// To add a new service type to this test, simply add an entry to the tests table
// with a function that creates and returns the service instance.
func TestServiceTypesConformToPlatformObject(t *testing.T) {
	tests := []struct {
		name     string
		instance common.PlatformObject
	}{
		{
			name: "Auth",
			instance: &Auth{
				ObjectMeta: metav1.ObjectMeta{
					Name: "auth",
				},
				Spec: AuthSpec{
					AdminGroups:   []string{"admin-group"},
					AllowedGroups: []string{"allowed-group"},
				},
				Status: AuthStatus{
					Status: common.Status{
						Phase: "Ready",
					},
				},
			},
		},
		{
			name: "Gateway",
			instance: &Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gateway",
				},
				Spec: GatewaySpec{
					Namespace: "openshift-ingress",
					Domain:    "example.com",
					Certificates: GatewayCertSpec{
						Type: "cert-manager",
					},
				},
				Status: GatewayStatus{
					Status: common.Status{
						Phase: "Ready",
					},
				},
			},
		},
		{
			name: "Monitoring",
			instance: &Monitoring{
				ObjectMeta: metav1.ObjectMeta{
					Name: "monitoring",
				},
				Spec: MonitoringSpec{
					MonitoringCommonSpec: MonitoringCommonSpec{
						Namespace: "opendatahub-monitoring",
					},
				},
				Status: MonitoringStatus{
					Status: common.Status{
						Phase: "Ready",
					},
				},
			},
		},
		{
			name: "ServiceMesh",
			instance: &ServiceMesh{
				ObjectMeta: metav1.ObjectMeta{
					Name: "servicemesh",
				},
				Spec: ServiceMeshSpec{
					ControlPlane: ServiceMeshControlPlaneSpec{
						Namespace: "istio-system",
					},
				},
				Status: ServiceMeshStatus{
					Status: common.Status{
						Phase: "Ready",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			// Test that the service type implements PlatformObject interface
			var platformObj common.PlatformObject = tt.instance
			g.Expect(platformObj).ToNot(BeNil())

			// Test GetStatus method
			status := tt.instance.GetStatus()
			g.Expect(status).ToNot(BeNil())
			g.Expect(status.Phase).To(Equal("Ready"))

			// Test condition methods
			conditions := tt.instance.GetConditions()
			g.Expect(conditions).To(BeEmpty()) // Initially empty

			// Set conditions and verify
			testConditions := []common.Condition{
				{
					Type:   "Ready",
					Status: "True",
				},
			}
			tt.instance.SetConditions(testConditions)

			retrievedConditions := tt.instance.GetConditions()
			g.Expect(retrievedConditions).To(HaveLen(1))
			g.Expect(retrievedConditions[0].Type).To(Equal("Ready"))
			g.Expect(string(retrievedConditions[0].Status)).To(Equal("True"))
		})
	}
}
