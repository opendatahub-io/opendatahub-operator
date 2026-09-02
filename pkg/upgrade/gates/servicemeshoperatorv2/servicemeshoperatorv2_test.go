package servicemeshoperatorv2_test

import (
	"errors"
	"testing"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	servicemeshoperatorv2gate "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/servicemeshoperatorv2"

	. "github.com/onsi/gomega"
)

func serviceMeshOperatorV2Scheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	s := runtime.NewScheme()
	g := NewWithT(t)
	g.Expect(operatorsv1alpha1.AddToScheme(s)).To(Succeed())

	return s
}

func TestCheck_PassesWhenSubscriptionMissing(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	cli := fake.NewClientBuilder().
		WithScheme(serviceMeshOperatorV2Scheme(t)).
		Build()

	err := servicemeshoperatorv2gate.Check(t.Context(), cli, "", "")
	g.Expect(err).ToNot(HaveOccurred())
}

func TestCheck_BlocksWhenLegacySubscriptionExists(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	cli := fake.NewClientBuilder().
		WithScheme(serviceMeshOperatorV2Scheme(t)).
		WithObjects(&operatorsv1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "custom-service-mesh-subscription",
				Namespace: "openshift-operators",
			},
			Spec: &operatorsv1alpha1.SubscriptionSpec{
				Package: "servicemeshoperator",
				Channel: "stable",
			},
			Status: operatorsv1alpha1.SubscriptionStatus{
				InstalledCSV: "servicemeshoperator.v2.6.17",
			},
		}).
		Build()

	err := servicemeshoperatorv2gate.Check(t.Context(), cli, "", "")
	g.Expect(err).To(HaveOccurred())

	var blockingErr *servicemeshoperatorv2gate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.SubscriptionNamespace).To(Equal("openshift-operators"))
	g.Expect(blockingErr.SubscriptionName).To(Equal("custom-service-mesh-subscription"))
	g.Expect(blockingErr.Channel).To(Equal("stable"))
	g.Expect(blockingErr.InstalledCSV).To(Equal("servicemeshoperator.v2.6.17"))
}

func TestCheck_BlocksWhenMatchingPackageHasNoChannel(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	cli := fake.NewClientBuilder().
		WithScheme(serviceMeshOperatorV2Scheme(t)).
		WithObjects(&operatorsv1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "servicemeshoperator",
				Namespace: "openshift-operators",
			},
			Spec: &operatorsv1alpha1.SubscriptionSpec{
				Package: "servicemeshoperator",
			},
		}).
		Build()

	err := servicemeshoperatorv2gate.Check(t.Context(), cli, "", "")
	g.Expect(err).To(HaveOccurred())
}

func TestCheck_IgnoresMetadataNameWhenPackageDoesNotMatch(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	cli := fake.NewClientBuilder().
		WithScheme(serviceMeshOperatorV2Scheme(t)).
		WithObjects(&operatorsv1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "servicemeshoperator",
				Namespace: "openshift-operators",
			},
			Spec: &operatorsv1alpha1.SubscriptionSpec{
				Package: "servicemeshoperator3",
			},
		}).
		Build()

	err := servicemeshoperatorv2gate.Check(t.Context(), cli, "", "")
	g.Expect(err).ToNot(HaveOccurred())
}
