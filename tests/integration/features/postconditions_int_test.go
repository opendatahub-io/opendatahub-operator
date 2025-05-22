package features_test

import (
	"context"

	corev1 "k8s.io/api/core/v1"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/integration/features/fixtures"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("feature postconditions", func() {
	Context("wait for pods to be ready", func() {
		var (
			objectCleaner *envtestutil.Cleaner
			namespace     string
			dsci          *dsciv1.DSCInitialization
		)

		BeforeEach(func(ctx context.Context) {
			objectCleaner = envtestutil.CreateCleaner(envTestClient, envTest.Config, fixtures.Timeout, fixtures.Interval)

			testFeatureName := "test-pods-ready"
			namespace = envtestutil.AppendRandomNameTo(testFeatureName)
			dsciName := envtestutil.AppendRandomNameTo(testFeatureName)
			dsci = fixtures.NewDSCInitialization(ctx, envTestClient, dsciName, namespace)
		})

		AfterEach(func(ctx context.Context) {
			objectCleaner.DeleteAll(ctx, dsci)
		})

		It("should succeed when all pods in the namespace are ready", func(ctx context.Context) {
			// given
			ns := fixtures.NewNamespace(namespace)
			Expect(envTestClient.Create(ctx, ns)).To(Succeed())

			podReady, err := fixtures.CreatePod(ctx, envTestClient, namespace, "test-pod")
			Expect(err).ToNot(HaveOccurred())

			podReady.Status.Phase = corev1.PodSucceeded

			Expect(envTestClient.Status().Update(ctx, podReady)).To(Succeed())

			// when
			featuresHandler := feature.ClusterFeaturesHandler(dsci, func(registry feature.FeaturesRegistry) error {
				errFeatureAdd := registry.Add(feature.Define("check-pods-ready").
					PostConditions(feature.WaitForPodsToBeReady(namespace)),
				)

				Expect(errFeatureAdd).ToNot(HaveOccurred())

				return nil
			})

			// then
			Expect(featuresHandler.Apply(ctx, envTestClient)).To(Succeed())
		})

		It("should succeed when there are evicted pods in the namespace", func(ctx context.Context) {
			// given
			ns := fixtures.NewNamespace(namespace)
			Expect(envTestClient.Create(ctx, ns)).To(Succeed())

			podReady, err := fixtures.CreatePod(ctx, envTestClient, namespace, "test-pod")
			Expect(err).ToNot(HaveOccurred())

			podReady.Status.Phase = corev1.PodSucceeded

			Expect(envTestClient.Status().Update(ctx, podReady)).To(Succeed())

			podEvicted, err := fixtures.CreatePod(ctx, envTestClient, namespace, "test-pod-evicted")
			Expect(err).ToNot(HaveOccurred())

			podEvicted.Status.Phase = corev1.PodFailed
			podEvicted.Status.Reason = "Evicted"
			podEvicted.Status.Conditions = []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
			}

			Expect(envTestClient.Status().Update(ctx, podEvicted)).To(Succeed())

			// when
			featuresHandler := feature.ClusterFeaturesHandler(dsci, func(registry feature.FeaturesRegistry) error {
				errFeatureAdd := registry.Add(feature.Define("check-pods-ready").
					PostConditions(feature.WaitForPodsToBeReady(namespace)),
				)

				Expect(errFeatureAdd).ToNot(HaveOccurred())

				return nil
			})

			// then
			Expect(featuresHandler.Apply(ctx, envTestClient)).To(Succeed())
		})

		It("should fail when there are no pods ready in the namespace", func(ctx context.Context) {
			// given
			ns := fixtures.NewNamespace(namespace)
			Expect(envTestClient.Create(ctx, ns)).To(Succeed())

			podNotReady, err := fixtures.CreatePod(ctx, envTestClient, namespace, "test-pod-not-ready")
			Expect(err).ToNot(HaveOccurred())

			podNotReady.Status.Phase = corev1.PodFailed
			podNotReady.Status.Reason = "Evicted"
			podNotReady.Status.Conditions = []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
			}

			Expect(envTestClient.Status().Update(ctx, podNotReady)).To(Succeed())

			// when
			featuresHandler := feature.ClusterFeaturesHandler(dsci, func(registry feature.FeaturesRegistry) error {
				errFeatureAdd := registry.Add(feature.Define("check-pods-ready").
					PostConditions(feature.WaitForPodsToBeReady(namespace)),
				)

				Expect(errFeatureAdd).ToNot(HaveOccurred())

				return nil
			})

			// then
			Expect(featuresHandler.Apply(ctx, envTestClient)).To(Not(Succeed()))
		})
	})
})
