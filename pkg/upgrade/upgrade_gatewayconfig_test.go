package upgrade_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestMigrateGatewayConfigIngressModeIdempotence_NoGatewayConfig(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	// First run - no GatewayConfig
	err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
	g.Expect(err).ShouldNot(HaveOccurred())

	stateAfterFirstRun, err := captureGatewayConfigState(ctx, cli)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(stateAfterFirstRun.Exists).To(BeFalse(), "GatewayConfig should not exist")

	// Verify idempotence with 2 additional runs
	verifyGatewayConfigIdempotence(ctx, g, cli, 2, stateAfterFirstRun)
}

func TestMigrateGatewayConfigIngressModeIdempotence_WithLoadBalancer(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// Create GatewayConfig without ingressMode
	gatewayConfig := createTestGatewayConfig()

	// Create LoadBalancer service
	gatewayService := createTestGatewayService(corev1.ServiceTypeLoadBalancer)

	cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig, gatewayService))
	g.Expect(err).ShouldNot(HaveOccurred())

	// First run - should set ingressMode
	err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
	g.Expect(err).ShouldNot(HaveOccurred())

	stateAfterFirstRun, err := captureGatewayConfigState(ctx, cli)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(stateAfterFirstRun.Exists).To(BeTrue())
	g.Expect(stateAfterFirstRun.IngressMode).To(Equal("LoadBalancer"), "ingressMode should be set to LoadBalancer")

	// Verify idempotence with 2 additional runs
	verifyGatewayConfigIdempotence(ctx, g, cli, 2, stateAfterFirstRun)
}

func TestMigrateGatewayConfigIngressModeIdempotence_AlreadyHasIngressMode(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// Create GatewayConfig with ingressMode already set
	gatewayConfig := createTestGatewayConfig()
	spec := map[string]interface{}{
		"ingressMode": "LoadBalancer",
	}
	err := unstructured.SetNestedMap(gatewayConfig.Object, spec, "spec")
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create LoadBalancer service
	gatewayService := createTestGatewayService(corev1.ServiceTypeLoadBalancer)

	cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig, gatewayService))
	g.Expect(err).ShouldNot(HaveOccurred())

	// First run - should be no-op
	err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
	g.Expect(err).ShouldNot(HaveOccurred())

	stateAfterFirstRun, err := captureGatewayConfigState(ctx, cli)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(stateAfterFirstRun.Exists).To(BeTrue())
	g.Expect(stateAfterFirstRun.IngressMode).To(Equal("LoadBalancer"), "ingressMode should remain LoadBalancer")

	// Verify idempotence with 2 additional runs
	verifyGatewayConfigIdempotence(ctx, g, cli, 2, stateAfterFirstRun)
}

func TestMigrateGatewayConfigIngressModeIdempotence_NoGatewayService(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// Create GatewayConfig without ingressMode
	gatewayConfig := createTestGatewayConfig()

	// No Gateway service created

	cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig))
	g.Expect(err).ShouldNot(HaveOccurred())

	// First run - should be no-op (no service)
	err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
	g.Expect(err).ShouldNot(HaveOccurred())

	stateAfterFirstRun, err := captureGatewayConfigState(ctx, cli)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(stateAfterFirstRun.Exists).To(BeTrue())
	g.Expect(stateAfterFirstRun.IngressMode).To(BeEmpty(), "ingressMode should remain empty")

	// Verify idempotence with 2 additional runs
	verifyGatewayConfigIdempotence(ctx, g, cli, 2, stateAfterFirstRun)
}

func TestMigrateGatewayConfigIngressModeIdempotence_NonLoadBalancerServices(t *testing.T) {
	ctx := t.Context()

	testCases := []struct {
		name        string
		serviceType corev1.ServiceType
	}{
		{"ClusterIP", corev1.ServiceTypeClusterIP},
		{"NodePort", corev1.ServiceTypeNodePort},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			// Create GatewayConfig without ingressMode
			gatewayConfig := createTestGatewayConfig()

			// Create service with specified type (not LoadBalancer)
			gatewayService := createTestGatewayService(tc.serviceType)

			cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig, gatewayService))
			g.Expect(err).ShouldNot(HaveOccurred())

			// First run - should be no-op (not LoadBalancer)
			err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
			g.Expect(err).ShouldNot(HaveOccurred())

			stateAfterFirstRun, err := captureGatewayConfigState(ctx, cli)
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(stateAfterFirstRun.Exists).To(BeTrue())
			g.Expect(stateAfterFirstRun.IngressMode).To(BeEmpty(), "ingressMode should remain empty for "+tc.name+" service")

			// Verify idempotence with 2 additional runs
			verifyGatewayConfigIdempotence(ctx, g, cli, 2, stateAfterFirstRun)
		})
	}
}

func TestMigrateGatewayConfigIngressModeIdempotence_ServiceAddedBetweenRuns(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// Create GatewayConfig without ingressMode
	gatewayConfig := createTestGatewayConfig()

	// No service initially
	cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig))
	g.Expect(err).ShouldNot(HaveOccurred())

	// First run - no service, should be no-op
	err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
	g.Expect(err).ShouldNot(HaveOccurred())

	stateAfterFirstRun, err := captureGatewayConfigState(ctx, cli)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(stateAfterFirstRun.Exists).To(BeTrue())
	g.Expect(stateAfterFirstRun.IngressMode).To(BeEmpty(), "ingressMode should be empty without service")

	// Add LoadBalancer service
	gatewayService := createTestGatewayService(corev1.ServiceTypeLoadBalancer)
	err = cli.Create(ctx, gatewayService)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Second run - service now exists, should set ingressMode
	err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
	g.Expect(err).ShouldNot(HaveOccurred())

	stateAfterSecondRun, err := captureGatewayConfigState(ctx, cli)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(stateAfterSecondRun.Exists).To(BeTrue())
	g.Expect(stateAfterSecondRun.IngressMode).To(Equal("LoadBalancer"), "ingressMode should be set with service present")

	// Verify idempotence with 2 additional runs
	verifyGatewayConfigIdempotence(ctx, g, cli, 2, stateAfterSecondRun)
}

func TestMigrateGatewayConfigIngressModeIdempotence_UserModifiesBetweenRuns(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	// Create GatewayConfig without ingressMode
	gatewayConfig := createTestGatewayConfig()

	// Create LoadBalancer service
	gatewayService := createTestGatewayService(corev1.ServiceTypeLoadBalancer)

	cli, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig, gatewayService))
	g.Expect(err).ShouldNot(HaveOccurred())

	// First run - should set ingressMode to LoadBalancer
	err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
	g.Expect(err).ShouldNot(HaveOccurred())

	stateAfterFirstRun, err := captureGatewayConfigState(ctx, cli)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(stateAfterFirstRun.IngressMode).To(Equal("LoadBalancer"))

	// Simulate user changing ingressMode to a custom value
	updatedGatewayConfig := createTestGatewayConfig()
	err = cli.Get(ctx, client.ObjectKey{Name: "default-gateway"}, updatedGatewayConfig)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = unstructured.SetNestedField(updatedGatewayConfig.Object, "Route", "spec", "ingressMode")
	g.Expect(err).ShouldNot(HaveOccurred())
	err = cli.Update(ctx, updatedGatewayConfig)
	g.Expect(err).ShouldNot(HaveOccurred())

	// Verify user's change
	stateAfterUserChange, err := captureGatewayConfigState(ctx, cli)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(stateAfterUserChange.IngressMode).To(Equal("Route"), "User's custom ingressMode should be set")

	// Second run - should preserve user's change (not overwrite)
	err = upgrade.MigrateGatewayConfigIngressMode(ctx, cli)
	g.Expect(err).ShouldNot(HaveOccurred())

	stateAfterSecondRun, err := captureGatewayConfigState(ctx, cli)
	g.Expect(err).ShouldNot(HaveOccurred())

	identical, differences := compareGatewayConfigStates(stateAfterUserChange, stateAfterSecondRun)
	g.Expect(identical).To(BeTrue(), "User's custom ingressMode should be preserved. Differences: %v", differences)
	g.Expect(stateAfterSecondRun.IngressMode).To(Equal("Route"), "User's custom ingressMode should not be overwritten")

	// Verify continued preservation with 1 additional run
	verifyGatewayConfigIdempotence(ctx, g, cli, 1, stateAfterSecondRun)
}
