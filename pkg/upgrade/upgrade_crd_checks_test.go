package upgrade_test

import (
	"context"
	"errors"
	"testing"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestCleanupExistingResourceWithCRDChecks(t *testing.T) {
	ctx := t.Context()

	t.Run("should complete without error when CRD checks succeed", func(t *testing.T) {
		g := NewWithT(t)

		// Create DSCI
		dsci := &unstructured.Unstructured{}
		dsci.SetGroupVersionKind(gvk.DSCInitialization)
		dsci.SetName("test-dsci")
		dsci.SetNamespace("test-namespace")
		err := unstructured.SetNestedField(dsci.Object, "test-app-ns", "spec", "applicationsNamespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Create OdhDashboardConfig for migration
		odhConfig := createTestOdhDashboardConfig(t, "test-app-ns")

		// Create AcceleratorProfile to migrate
		ap := createTestAcceleratorProfile(t, "test-app-ns")

		// Create fake client - types are registered in the scheme, so HasCRD will succeed
		cli, err := fakeclient.New(fakeclient.WithObjects(dsci, odhConfig, ap))
		g.Expect(err).ShouldNot(HaveOccurred())

		// Call CleanupExistingResource - should complete without error
		// The actual migration behavior is tested in TestMigrateAcceleratorProfilesToHardwareProfiles
		err = upgrade.CleanupExistingResource(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should skip HardwareProfile migration when infrastructure HWP CRD does not exist", func(t *testing.T) {
		g := NewWithT(t)

		// Create DSCI
		dsci := &unstructured.Unstructured{}
		dsci.SetGroupVersionKind(gvk.DSCInitialization)
		dsci.SetName("test-dsci")
		dsci.SetNamespace("test-namespace")
		err := unstructured.SetNestedField(dsci.Object, "test-app-ns", "spec", "applicationsNamespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Create AcceleratorProfile but simulate missing infrastructure HWP CRD
		ap := createTestAcceleratorProfile(t, "test-app-ns")

		// Intercept HasCRD to simulate missing infrastructure HWP CRD
		interceptorFuncs := interceptor.Funcs{
			Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				// Simulate CRD not found for infrastructure HardwareProfile
				if key.Name == "hardwareprofiles.infrastructure.opendatahub.io" {
					return k8serr.NewNotFound(schema.GroupResource{
						Group:    "apiextensions.k8s.io",
						Resource: "customresourcedefinitions",
					}, key.Name)
				}
				return client.Get(ctx, key, obj, opts...)
			},
		}

		cli, err := fakeclient.New(
			fakeclient.WithObjects(dsci, ap),
			fakeclient.WithInterceptorFuncs(interceptorFuncs),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.CleanupExistingResource(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify NO HardwareProfiles were created
		var hwpList infrav1.HardwareProfileList
		err = cli.List(ctx, &hwpList, client.InNamespace("test-app-ns"))
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(hwpList.Items).To(BeEmpty(), "HardwareProfiles should not be created when infrastructure CRD missing")
	})

	t.Run("should skip HardwareProfile migration when AcceleratorProfile CRD does not exist", func(t *testing.T) {
		g := NewWithT(t)

		// Create DSCI
		dsci := &unstructured.Unstructured{}
		dsci.SetGroupVersionKind(gvk.DSCInitialization)
		dsci.SetName("test-dsci")
		dsci.SetNamespace("test-namespace")
		err := unstructured.SetNestedField(dsci.Object, "test-app-ns", "spec", "applicationsNamespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Intercept HasCRD to simulate missing AcceleratorProfile CRD
		interceptorFuncs := interceptor.Funcs{
			Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				// Simulate CRD not found for dashboard AcceleratorProfile
				if key.Name == "acceleratorprofiles.dashboard.opendatahub.io" {
					return k8serr.NewNotFound(schema.GroupResource{
						Group:    "apiextensions.k8s.io",
						Resource: "customresourcedefinitions",
					}, key.Name)
				}
				return client.Get(ctx, key, obj, opts...)
			},
		}

		cli, err := fakeclient.New(
			fakeclient.WithObjects(dsci),
			fakeclient.WithInterceptorFuncs(interceptorFuncs),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.CleanupExistingResource(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify NO HardwareProfiles were created
		var hwpList infrav1.HardwareProfileList
		err = cli.List(ctx, &hwpList, client.InNamespace("test-app-ns"))
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(hwpList.Items).To(BeEmpty(), "HardwareProfiles should not be created when AcceleratorProfile CRD missing")
	})

	t.Run("should run GatewayConfig migration when GatewayConfig CRD exists", func(t *testing.T) {
		g := NewWithT(t)

		// Create DSCI
		dsci := &unstructured.Unstructured{}
		dsci.SetGroupVersionKind(gvk.DSCInitialization)
		dsci.SetName("test-dsci")
		dsci.SetNamespace("test-namespace")
		err := unstructured.SetNestedField(dsci.Object, "test-app-ns", "spec", "applicationsNamespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Create GatewayConfig without ingressMode set
		gatewayConfig := createTestGatewayConfig()

		cli, err := fakeclient.New(fakeclient.WithObjects(dsci, gatewayConfig))
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.CleanupExistingResource(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		// GatewayConfig migration should have been attempted (no error means it ran)
		// We can't verify the migration result without creating a Gateway service,
		// but we verify no error occurred
	})

	t.Run("should skip GatewayConfig migration when GatewayConfig CRD does not exist", func(t *testing.T) {
		g := NewWithT(t)

		// Create DSCI
		dsci := &unstructured.Unstructured{}
		dsci.SetGroupVersionKind(gvk.DSCInitialization)
		dsci.SetName("test-dsci")
		dsci.SetNamespace("test-namespace")
		err := unstructured.SetNestedField(dsci.Object, "test-app-ns", "spec", "applicationsNamespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Intercept HasCRD to simulate missing GatewayConfig CRD
		interceptorFuncs := interceptor.Funcs{
			Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				// Simulate CRD not found for GatewayConfig
				if key.Name == "gatewayconfigs.services.platform.opendatahub.io" {
					return k8serr.NewNotFound(schema.GroupResource{
						Group:    "apiextensions.k8s.io",
						Resource: "customresourcedefinitions",
					}, key.Name)
				}
				return client.Get(ctx, key, obj, opts...)
			},
		}

		cli, err := fakeclient.New(
			fakeclient.WithObjects(dsci),
			fakeclient.WithInterceptorFuncs(interceptorFuncs),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.CleanupExistingResource(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		// No error should occur, migration should be skipped silently
	})

	t.Run("should handle CRD check errors gracefully", func(t *testing.T) {
		g := NewWithT(t)

		// Create DSCI
		dsci := &unstructured.Unstructured{}
		dsci.SetGroupVersionKind(gvk.DSCInitialization)
		dsci.SetName("test-dsci")
		dsci.SetNamespace("test-namespace")
		err := unstructured.SetNestedField(dsci.Object, "test-app-ns", "spec", "applicationsNamespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		// Intercept to simulate error checking CRD
		interceptorFuncs := interceptor.Funcs{
			Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if key.Name == "hardwareprofiles.infrastructure.opendatahub.io" {
					return errors.New("simulated API server error")
				}
				return client.Get(ctx, key, obj, opts...)
			},
		}

		cli, err := fakeclient.New(
			fakeclient.WithObjects(dsci),
			fakeclient.WithInterceptorFuncs(interceptorFuncs),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.CleanupExistingResource(ctx, cli)

		// Should return error containing CRD check failure
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("failed to check HardwareProfile CRD"))
	})

	t.Run("should handle no DSCI gracefully", func(t *testing.T) {
		g := NewWithT(t)

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		err = upgrade.CleanupExistingResource(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())
	})
}
