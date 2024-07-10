package features_test

import (
	"context"
	"path"

	corev1 "k8s.io/api/core/v1"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/integration/features/fixtures"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Applying and updating resources", func() {
	var (
		testNamespace string
		namespace     *corev1.Namespace
		objectCleaner *envtestutil.Cleaner
		dsci          *dsciv1.DSCInitialization
	)

	const (
		testAnnotationKey = "test-annotation"
		newValue          = "new-value"
		originalValue     = "original-value"
	)

	BeforeEach(func(ctx context.Context) {
		objectCleaner = envtestutil.CreateCleaner(envTestClient, envTest.Config, fixtures.Timeout, fixtures.Interval)

		testNamespace = envtestutil.AppendRandomNameTo("test-namespace")

		var err error
		namespace, err = cluster.CreateNamespace(ctx, envTestClient, testNamespace)
		Expect(err).ToNot(HaveOccurred())

		dsci = fixtures.NewDSCInitialization(testNamespace)
		dsci.Spec.ServiceMesh.ControlPlane.Namespace = namespace.Name
	})

	AfterEach(func(ctx context.Context) {
		objectCleaner.DeleteAll(ctx, namespace)
	})

	When("a feature is managed", func() {
		It("should reconcile the resource to its managed state", func(ctx context.Context) {
			// given managed feature
			featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
				return feature.CreateFeature("create-local-gw-svc").
					For(handler).
					UsingConfig(envTest.Config).
					Managed().
					ManifestsLocation(fixtures.TestEmbeddedFiles).
					Manifests(path.Join(fixtures.BaseDir, "local-gateway-svc.tmpl.yaml")).
					Load()

			})
			Expect(featuresHandler.Apply(ctx)).To(Succeed())

			// expect created svc to have managed annotation
			service, err := fixtures.GetService(ctx, envTestClient, testNamespace, "knative-local-gateway")
			Expect(err).ToNot(HaveOccurred())
			Expect(service.Annotations).To(
				HaveKeyWithValue(annotations.ManagedByODHOperator, "true"),
			)

			// when
			service.Annotations[testAnnotationKey] = newValue
			Expect(envTestClient.Update(ctx, service)).To(Succeed())

			// then
			// expect that modification is reconciled away
			Expect(featuresHandler.Apply(ctx)).To(Succeed())
			updatedService, err := fixtures.GetService(ctx, envTestClient, testNamespace, "knative-local-gateway")
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedService.Annotations).To(
				HaveKeyWithValue(testAnnotationKey, originalValue),
			)
		})

		It("should not reconcile explicitly opt-ed out resource", func(ctx context.Context) {
			// given managed feature
			featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
				return feature.CreateFeature("create-unmanaged-svc").
					For(handler).
					UsingConfig(envTest.Config).
					Managed().
					ManifestsLocation(fixtures.TestEmbeddedFiles).
					Manifests(path.Join(fixtures.BaseDir, "unmanaged-svc.tmpl.yaml")).
					Load()

			})
			Expect(featuresHandler.Apply(ctx)).To(Succeed())

			// when
			service, err := fixtures.GetService(ctx, envTestClient, testNamespace, "unmanaged-svc")
			Expect(err).ToNot(HaveOccurred())
			service.Annotations[testAnnotationKey] = newValue
			Expect(envTestClient.Update(ctx, service)).To(Succeed())

			// then
			// expect that modification is reconciled away
			Expect(featuresHandler.Apply(ctx)).To(Succeed())
			updatedService, err := fixtures.GetService(ctx, envTestClient, testNamespace, "unmanaged-svc")
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedService.Annotations).To(
				HaveKeyWithValue(testAnnotationKey, newValue),
			)
		})

	})

	When("a feature is unmanaged", func() {

		It("should not reconcile the resource", func(ctx context.Context) {
			// given unmanaged feature
			featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
				return feature.CreateFeature("create-local-gw-svc").
					For(handler).
					UsingConfig(envTest.Config).
					ManifestsLocation(fixtures.TestEmbeddedFiles).
					Manifests(path.Join(fixtures.BaseDir, "local-gateway-svc.tmpl.yaml")).
					Load()
			})
			Expect(featuresHandler.Apply(ctx)).To(Succeed())

			// when
			service, err := fixtures.GetService(ctx, envTestClient, testNamespace, "knative-local-gateway")
			Expect(err).ToNot(HaveOccurred())
			service.Annotations[testAnnotationKey] = newValue
			Expect(envTestClient.Update(ctx, service)).To(Succeed())

			// then
			// expect that modification is reconciled away
			Expect(featuresHandler.Apply(ctx)).To(Succeed())
			updatedService, err := fixtures.GetService(ctx, envTestClient, testNamespace, "knative-local-gateway")
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedService.Annotations).To(
				HaveKeyWithValue(testAnnotationKey, newValue),
			)

		})
	})

	When("a feature is unmanaged but the object is marked as managed", func() {
		It("should reconcile this resource", func(ctx context.Context) {
			// given unmanaged feature but object marked with managed annotation
			featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
				return feature.CreateFeature("create-managed-svc").
					For(handler).
					UsingConfig(envTest.Config).
					ManifestsLocation(fixtures.TestEmbeddedFiles).
					Manifests(path.Join(fixtures.BaseDir, "managed-svc.tmpl.yaml")).
					Load()
			})
			Expect(featuresHandler.Apply(ctx)).To(Succeed())

			// when
			service, err := fixtures.GetService(ctx, envTestClient, testNamespace, "managed-svc")
			Expect(err).ToNot(HaveOccurred())
			service.Annotations[testAnnotationKey] = newValue
			Expect(envTestClient.Update(ctx, service)).To(Succeed())

			// then
			// expect that modification is reconciled away
			Expect(featuresHandler.Apply(ctx)).To(Succeed())
			updatedService, err := fixtures.GetService(ctx, envTestClient, testNamespace, "managed-svc")
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedService.Annotations).To(
				HaveKeyWithValue(testAnnotationKey, originalValue),
			)
		})
	})

})
