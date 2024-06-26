package features_test

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/integration/features/fixtures"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("feature preconditions", func() {

	Context("namespace existence", func() {

		var (
			objectCleaner *envtestutil.Cleaner
			namespace     string
			dsci          *dsciv1.DSCInitialization
		)

		BeforeEach(func() {
			objectCleaner = envtestutil.CreateCleaner(envTestClient, envTest.Config, fixtures.Timeout, fixtures.Interval)

			testFeatureName := "test-ns-creation"
			namespace = envtestutil.AppendRandomNameTo(testFeatureName)
			dsci = fixtures.NewDSCInitialization(namespace)
		})

		It("should create namespace if it does not exist", func(ctx context.Context) {
			// given
			_, err := fixtures.GetNamespace(ctx, envTestClient, namespace)
			Expect(k8serr.IsNotFound(err)).To(BeTrue())
			defer objectCleaner.DeleteAll(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})

			// when
			featuresHandler := feature.ClusterFeaturesHandler(dsci, func(registry feature.FeaturesRegistry) error {
				testFeatureErr := registry.Add(feature.Define("create-new-ns").
					UsingConfig(envTest.Config).
					PreConditions(feature.CreateNamespaceIfNotExists(namespace)),
				)

				Expect(testFeatureErr).ToNot(HaveOccurred())

				return nil
			})

			// then
			Expect(featuresHandler.Apply(ctx)).To(Succeed())

			// and
			Eventually(func() error {
				_, err := fixtures.GetNamespace(ctx, envTestClient, namespace)
				return err
			}).
				WithTimeout(fixtures.Timeout).
				WithPolling(fixtures.Interval).
				Should(Succeed())
		})

		It("should not try to create namespace if it does already exist", func(ctx context.Context) {
			// given
			ns := fixtures.NewNamespace(namespace)
			Expect(envTestClient.Create(ctx, ns)).To(Succeed())
			Eventually(func() error {
				_, err := fixtures.GetNamespace(ctx, envTestClient, namespace)
				return err
			}).WithTimeout(fixtures.Timeout).WithPolling(fixtures.Interval).Should(Succeed()) // wait for ns to actually get created

			defer objectCleaner.DeleteAll(ctx, ns)

			// when
			featuresHandler := feature.ClusterFeaturesHandler(dsci, func(registry feature.FeaturesRegistry) error {
				testFeatureErr := registry.Add(feature.Define("create-new-ns").
					UsingConfig(envTest.Config).
					PreConditions(feature.CreateNamespaceIfNotExists(namespace)),
				)

				Expect(testFeatureErr).ToNot(HaveOccurred())

				return nil
			})

			// then
			Expect(featuresHandler.Apply(ctx)).To(Succeed())

		})

	})

})
