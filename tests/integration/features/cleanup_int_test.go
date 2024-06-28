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
			dsci        *dsciv1.DSCInitialization
			namespace   string
			testFeature *feature.Feature
		)

		BeforeAll(func() {
			namespace = envtestutil.AppendRandomNameTo("test-secret-ownership")
			dsci = fixtures.NewDSCInitialization(namespace)
			var errSecretCreation error
			testFeature, errSecretCreation = feature.Define(featureName).
				TargetNamespace(dsci.Spec.ApplicationsNamespace).
				Source(featurev1.Source{
					Type: featurev1.DSCIType,
					Name: dsci.Name,
				}).
				UsingConfig(envTest.Config).
				PreConditions(
					feature.CreateNamespaceIfNotExists(namespace),
				).
				WithResources(fixtures.CreateSecret(secretName, namespace)).
				Create()

			Expect(errSecretCreation).ToNot(HaveOccurred())

		})

		It("should successfully create resource and associated feature tracker", func(ctx context.Context) {
			// when
			Expect(testFeature.Apply(ctx)).Should(Succeed())

			// then
			Eventually(createdSecretHasOwnerReferenceToOwningFeature(namespace, secretName)).
				WithContext(ctx).
				WithTimeout(fixtures.Timeout).
				WithPolling(fixtures.Interval).
				Should(Succeed())
		})

		It("should remove feature tracker on clean-up", func(ctx context.Context) {
			// when
			Expect(testFeature.Cleanup(ctx)).To(Succeed())

			// then
			Eventually(createdSecretHasOwnerReferenceToOwningFeature(namespace, secretName)).
				WithContext(ctx).
				WithTimeout(fixtures.Timeout).
				WithPolling(fixtures.Interval).
				Should(WithTransform(errors.IsNotFound, BeTrue()))
		})

	})

})

func createdSecretHasOwnerReferenceToOwningFeature(namespace, secretName string) func(context.Context) error {
	return func(ctx context.Context) error {
		secret, err := envTestClientset.CoreV1().
			Secrets(namespace).
			Get(ctx, secretName, metav1.GetOptions{})

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
		return envTestClient.Get(ctx, client.ObjectKey{
			Name: trackerName,
		}, tracker)
	}
}
