package certmanager_test

import (
	"errors"
	"testing"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	certmanagergate "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/certmanager"

	. "github.com/onsi/gomega"
)

func certManagerScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	s := runtime.NewScheme()
	g := NewWithT(t)
	g.Expect(v1alpha1.AddToScheme(s)).To(Succeed())

	return s
}

func TestCheck_PassesWhenSubscriptionExists(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	cli := fake.NewClientBuilder().
		WithScheme(certManagerScheme(t)).
		WithObjects(&v1alpha1.Subscription{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "openshift-cert-manager-operator",
				Namespace: "cert-manager-operator",
			},
		}).
		Build()

	err := certmanagergate.Check(t.Context(), cli, "", "")
	g.Expect(err).ToNot(HaveOccurred())
}

func TestCheck_BlocksWhenSubscriptionMissing(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	cli := fake.NewClientBuilder().
		WithScheme(certManagerScheme(t)).
		Build()

	err := certmanagergate.Check(t.Context(), cli, "", "")
	g.Expect(err).To(HaveOccurred())

	var blockingErr *certmanagergate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.SubscriptionNamespace).To(Equal("cert-manager-operator"))
	g.Expect(blockingErr.SubscriptionName).To(Equal("openshift-cert-manager-operator"))
}
