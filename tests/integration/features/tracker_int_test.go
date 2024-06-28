package features_test

import (
	"context"
	"errors"

	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/integration/features/fixtures"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
)

var _ = Describe("Feature tracking capability", func() {

	const appNamespace = "default"

	var (
		dsci *dsciv1.DSCInitialization
	)

	BeforeEach(func() {
		dsci = fixtures.NewDSCInitialization("default")
	})

	Context("Reporting progress when applying Feature", func() {

		It("should indicate successful installation in FeatureTracker through Status conditions", func(ctx context.Context) {
			featuresHandler := feature.ClusterFeaturesHandler(dsci, func(registry feature.FeaturesRegistry) error {
				verificationFeatureErr := registry.Add(
					feature.Define("always-working-feature").
						UsingConfig(envTest.Config),
				)

				Expect(verificationFeatureErr).ToNot(HaveOccurred())

				return nil
			})

			// when
			Expect(featuresHandler.Apply(ctx)).To(Succeed())

			// then
			featureTracker, err := fixtures.GetFeatureTracker(ctx, envTestClient, appNamespace, "always-working-feature")
			Expect(err).ToNot(HaveOccurred())
			Expect(featureTracker.Status.Phase).To(Equal(status.PhaseReady))
			Expect(featureTracker.Status.Conditions).To(ContainElement(
				MatchFields(IgnoreExtras, Fields{
					"Type":   Equal(conditionsv1.ConditionAvailable),
					"Status": Equal(corev1.ConditionTrue),
					"Reason": Equal(string(featurev1.ConditionReason.FeatureCreated)),
				}),
			))
		})

		It("should indicate when failure occurs in preconditions through Status conditions", func(ctx context.Context) {
			// given
			featuresHandler := feature.ClusterFeaturesHandler(dsci, func(registry feature.FeaturesRegistry) error {
				verificationFeatureErr := registry.Add(feature.Define("precondition-fail").
					UsingConfig(envTest.Config).
					PreConditions(func(_ context.Context, _ *feature.Feature) error {
						return errors.New("during test always fail")
					}),
				)

				Expect(verificationFeatureErr).ToNot(HaveOccurred())

				return nil
			})

			// when
			Expect(featuresHandler.Apply(ctx)).ToNot(Succeed())

			// then
			featureTracker, err := fixtures.GetFeatureTracker(ctx, envTestClient, appNamespace, "precondition-fail")
			Expect(err).ToNot(HaveOccurred())
			Expect(featureTracker.Status.Phase).To(Equal(status.PhaseError))
			Expect(featureTracker.Status.Conditions).To(ContainElement(
				MatchFields(IgnoreExtras, Fields{
					"Type":   Equal(conditionsv1.ConditionDegraded),
					"Status": Equal(corev1.ConditionTrue),
					"Reason": Equal(string(featurev1.ConditionReason.PreConditions)),
				}),
			))
		})

		It("should indicate when failure occurs in post-conditions through Status conditions", func(ctx context.Context) {
			// given
			featuresHandler := feature.ClusterFeaturesHandler(dsci, func(registry feature.FeaturesRegistry) error {
				verificationFeatureErr := registry.Add(feature.Define("post-condition-failure").
					UsingConfig(envTest.Config).
					PostConditions(func(_ context.Context, _ *feature.Feature) error {
						return errors.New("during test always fail")
					}),
				)

				Expect(verificationFeatureErr).ToNot(HaveOccurred())

				return nil
			})

			// when
			Expect(featuresHandler.Apply(ctx)).ToNot(Succeed())

			// then
			featureTracker, err := fixtures.GetFeatureTracker(ctx, envTestClient, appNamespace, "post-condition-failure")
			Expect(err).ToNot(HaveOccurred())
			Expect(featureTracker.Status.Phase).To(Equal(status.PhaseError))
			Expect(featureTracker.Status.Conditions).To(ContainElement(
				MatchFields(IgnoreExtras, Fields{
					"Type":   Equal(conditionsv1.ConditionDegraded),
					"Status": Equal(corev1.ConditionTrue),
					"Reason": Equal(string(featurev1.ConditionReason.PostConditions)),
				}),
			))
		})
	})

	Context("adding metadata of FeatureTracker origin", func() {

		It("should correctly indicate source in the feature tracker", func(ctx context.Context) {
			// given
			featuresHandler := feature.ClusterFeaturesHandler(dsci, func(registry feature.FeaturesRegistry) error {
				emptyFeatureErr := registry.Add(feature.Define("always-working-feature").
					UsingConfig(envTest.Config),
				)

				Expect(emptyFeatureErr).ToNot(HaveOccurred())

				return nil
			})

			// when
			Expect(featuresHandler.Apply(ctx)).To(Succeed())

			// then
			featureTracker, err := fixtures.GetFeatureTracker(ctx, envTestClient, appNamespace, "always-working-feature")
			Expect(err).ToNot(HaveOccurred())
			Expect(featureTracker.Spec.Source).To(
				MatchFields(IgnoreExtras, Fields{
					"Name": Equal("default-dsci"),
					"Type": Equal(featurev1.DSCIType),
				}),
			)
		})

		It("should correctly indicate app namespace in the feature tracker", func(ctx context.Context) {
			// given
			featuresHandler := feature.ClusterFeaturesHandler(dsci, func(registry feature.FeaturesRegistry) error {
				emptyFeatureErr := registry.Add(feature.Define("empty-feature").
					UsingConfig(envTest.Config),
				)

				Expect(emptyFeatureErr).ToNot(HaveOccurred())

				return nil
			})

			// when
			Expect(featuresHandler.Apply(ctx)).To(Succeed())

			// then
			featureTracker, err := fixtures.GetFeatureTracker(ctx, envTestClient, appNamespace, "empty-feature")
			Expect(err).ToNot(HaveOccurred())
			Expect(featureTracker.Spec.AppNamespace).To(Equal("default"))
		})

	})
})
