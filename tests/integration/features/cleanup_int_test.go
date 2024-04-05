package features_test

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/integration/features/fixtures"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
)

var _ = Describe("feature cleanup", func() {

	Context("using FeatureTracker and ownership as cleanup strategy", Ordered, func() {

		const (
			featureName = "create-secret"
			secretName  = "test-secret"
		)

		var (
			dsci            *dsciv1.DSCInitialization
			namespace       string
			featuresHandler *feature.FeaturesHandler
		)

		BeforeAll(func() {
			namespace = envtestutil.AppendRandomNameTo("test-secret-ownership")
			dsci = fixtures.NewDSCInitialization(namespace)
			featuresHandler = feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
				secretCreationErr := feature.CreateFeature(featureName).
					For(handler).
					UsingConfig(envTest.Config).
					PreConditions(
						feature.CreateNamespaceIfNotExists(namespace),
					).
					WithResources(fixtures.CreateSecret(secretName, namespace)).
					Load()

				Expect(secretCreationErr).ToNot(HaveOccurred())

				return nil
			})

		})

		It("should successfully create resource and associated feature tracker", func() {
			// when
			Expect(featuresHandler.Apply()).Should(Succeed())

			// then
			Eventually(createdSecretHasOwnerReferenceToOwningFeature(namespace, secretName)).
				WithTimeout(fixtures.Timeout).
				WithPolling(fixtures.Interval).
				Should(Succeed())
		})

		It("should remove feature tracker on clean-up", func() {
			// when
			Expect(featuresHandler.Delete()).To(Succeed())

			// then
			Eventually(createdSecretHasOwnerReferenceToOwningFeature(namespace, secretName)).
				WithTimeout(fixtures.Timeout).
				WithPolling(fixtures.Interval).
				Should(WithTransform(errors.IsNotFound, BeTrue()))
		})

	})

})

func createdSecretHasOwnerReferenceToOwningFeature(namespace, secretName string) func() error {
	return func() error {
		secret, err := envTestClientset.CoreV1().
			Secrets(namespace).
			Get(context.TODO(), secretName, metav1.GetOptions{})

		if err != nil {
			return err
		}

		Expect(secret.OwnerReferences).Should(
			ContainElement(
				MatchFields(IgnoreExtras, Fields{"Kind": Equal("FeatureTracker")}),
			),
		)

		trackerName := ""
		for _, ownerRef := range secret.OwnerReferences {
			if ownerRef.Kind == "FeatureTracker" {
				trackerName = ownerRef.Name
				break
			}
		}

		tracker := &featurev1.FeatureTracker{}
		return envTestClient.Get(context.Background(), client.ObjectKey{
			Name: trackerName,
		}, tracker)
	}
}
