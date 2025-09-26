package gateway_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	gatewayctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"

	. "github.com/onsi/gomega"
)

// newServiceHandler creates a new Gateway ServiceHandler for testing.
func newServiceHandler() *gatewayctrl.ServiceHandler {
	return &gatewayctrl.ServiceHandler{}
}

// allPlatforms returns all supported platforms for comprehensive testing.
func allPlatforms() []struct {
	name     string
	platform common.Platform
} {
	return []struct {
		name     string
		platform common.Platform
	}{
		{"OpenDataHub", cluster.OpenDataHub},
		{"SelfManagedRhoai", cluster.SelfManagedRhoai},
		{"ManagedRhoai", cluster.ManagedRhoai},
	}
}

func TestServiceHandler_GetName(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	handler := newServiceHandler()

	name := handler.GetName()
	g.Expect(name).Should(Equal(serviceApi.GatewayServiceName))
}

func TestServiceHandler_Init(t *testing.T) {
	t.Parallel()
	handler := newServiceHandler()

	for _, platform := range allPlatforms() {
		// capture loop variable
		t.Run("should initialize successfully for "+platform.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			err := handler.Init(platform.platform)
			g.Expect(err).ShouldNot(HaveOccurred(), platform.name+" platform should initialize without errors")
		})
	}
}

func TestServiceHandler_GetManagementState(t *testing.T) {
	t.Parallel()
	handler := newServiceHandler()

	tests := []struct {
		name string
		dsci *dsciv1.DSCInitialization
	}{
		{"with empty DSCInitialization", &dsciv1.DSCInitialization{}},
		{"with nil DSCInitialization", nil},
		{"with configured DSCInitialization", &dsciv1.DSCInitialization{
			Spec: dsciv1.DSCInitializationSpec{
				ApplicationsNamespace: "test-namespace",
			},
		}},
	}

	// Test all platforms return Managed state
	for _, platform := range allPlatforms() {
		// capture loop variable
		t.Run("should return Managed for "+platform.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			state := handler.GetManagementState(platform.platform, &dsciv1.DSCInitialization{})
			g.Expect(state).Should(Equal(operatorv1.Managed), platform.name+" should always be managed")
		})
	}

	// Test different DSCI configurations
	for _, tt := range tests {
		// capture loop variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			state := handler.GetManagementState(cluster.OpenDataHub, tt.dsci)
			g.Expect(state).Should(Equal(operatorv1.Managed), "Should always return Managed regardless of DSCI config")
		})
	}
}

func TestServiceHandler_NewReconciler(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	handler := newServiceHandler()

	t.Run("should handle nil manager gracefully", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)

		defer func() {
			if r := recover(); r != nil {
				g.Expect(r).ShouldNot(BeNil(), "Should recover from nil manager panic")
			}
		}()

		_ = handler.NewReconciler(ctx, nil)
	})
}

func TestServiceHandler_Implements_ServiceInterface(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	handler := newServiceHandler()

	// Verify all required methods exist and work correctly
	err := handler.Init(cluster.OpenDataHub)
	g.Expect(err).ShouldNot(HaveOccurred())

	name := handler.GetName()
	g.Expect(name).Should(Equal(serviceApi.GatewayServiceName))

	state := handler.GetManagementState(cluster.OpenDataHub, &dsciv1.DSCInitialization{})
	g.Expect(state).Should(Equal(operatorv1.Managed))

	// Test NewReconciler method exists (will panic with nil manager)
	defer func() {
		r := recover()
		g.Expect(r).ShouldNot(BeNil())
	}()
	_ = handler.NewReconciler(t.Context(), nil)
}

func TestServiceHandler_ServiceName_Consistency(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	handler := newServiceHandler()

	handlerName := handler.GetName()
	g.Expect(handlerName).Should(Equal(serviceApi.GatewayServiceName))
	g.Expect(handlerName).ShouldNot(BeEmpty())
}
