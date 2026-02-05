package upgrade_test

import (
	"context"
	"errors"
	"testing"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestGetOdhDashboardConfigWithMissingCRD(t *testing.T) {
	ctx := t.Context()

	t.Run("should handle NotFound error when OdhDashboardConfig resource doesn't exist", func(t *testing.T) {
		g := NewWithT(t)

		// Create a fake client with no OdhDashboardConfig
		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		// When not found in cluster, it attempts to load from manifests
		// If manifests don't exist, it will return a file not found error
		_, found, err := upgrade.GetOdhDashboardConfig(ctx, cli, "test-app-ns")

		// The function returns error when manifest file is not found
		// This is expected behavior when both cluster and manifest sources fail
		if err != nil {
			g.Expect(err.Error()).To(ContainSubstring("failed to load OdhDashboardConfig from manifests"))
		} else {
			// If somehow manifests are available in test env, found should be false
			g.Expect(found).To(BeFalse())
		}
	})

	t.Run("should handle NoMatchError when OdhDashboardConfig CRD is not installed", func(t *testing.T) {
		g := NewWithT(t)

		interceptorFuncs := interceptor.Funcs{
			Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				// Simulate the CRD not being installed
				if obj.GetObjectKind().GroupVersionKind().Kind == "OdhDashboardConfig" {
					return &meta.NoKindMatchError{
						GroupKind: schema.GroupKind{
							Group: "opendatahub.io",
							Kind:  "OdhDashboardConfig",
						},
						SearchedVersions: []string{"v1alpha1"},
					}
				}
				return client.Get(ctx, key, obj, opts...)
			},
		}

		cli, err := fakeclient.New(
			fakeclient.WithInterceptorFuncs(interceptorFuncs),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		// When NoMatchError occurs (CRD missing), it should not fail immediately
		// It attempts to load from manifests as a fallback
		_, found, err := upgrade.GetOdhDashboardConfig(ctx, cli, "test-app-ns")

		// If manifests don't exist, it will return error
		// This is expected behavior when both cluster and manifest sources fail
		if err != nil {
			g.Expect(err.Error()).To(ContainSubstring("failed to load OdhDashboardConfig from manifests"))
		} else {
			// If somehow manifests are available in test env, found should be false
			g.Expect(found).To(BeFalse())
		}
	})

	t.Run("should return error for other types of errors", func(t *testing.T) {
		g := NewWithT(t)

		interceptorFuncs := interceptor.Funcs{
			Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				// Simulate a different error (e.g., network error, permission error)
				if obj.GetObjectKind().GroupVersionKind().Kind == "OdhDashboardConfig" {
					return k8serr.NewInternalError(errors.New("internal server error"))
				}
				return client.Get(ctx, key, obj, opts...)
			},
		}

		cli, err := fakeclient.New(
			fakeclient.WithInterceptorFuncs(interceptorFuncs),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		// This should return the error since it's not NotFound or NoMatch
		_, _, err = upgrade.GetOdhDashboardConfig(ctx, cli, "test-app-ns")
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("failed to get OdhDashboardConfig from cluster"))
		g.Expect(err.Error()).To(ContainSubstring("internal server error"))
	})

	t.Run("should successfully get OdhDashboardConfig when it exists in cluster", func(t *testing.T) {
		g := NewWithT(t)

		odhConfig := createTestOdhDashboardConfig(t, "test-app-ns")

		cli, err := fakeclient.New(fakeclient.WithObjects(odhConfig))
		g.Expect(err).ShouldNot(HaveOccurred())

		// This should successfully retrieve the OdhDashboardConfig
		config, found, err := upgrade.GetOdhDashboardConfig(ctx, cli, "test-app-ns")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).To(BeTrue())
		g.Expect(config).ToNot(BeNil())
		g.Expect(config.GetName()).To(Equal("odh-dashboard-config"))
		g.Expect(config.GetNamespace()).To(Equal("test-app-ns"))
	})
}
