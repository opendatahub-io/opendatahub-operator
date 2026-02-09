package upgrade_test

import (
	"context"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestCleanupDeprecatedKueueVAPB(t *testing.T) {
	ctx := t.Context()

	t.Run("should delete existing ValidatingAdmissionPolicyBinding during upgrade cleanup", func(t *testing.T) {
		g := NewWithT(t)

		// Create a deprecated ValidatingAdmissionPolicyBinding
		vapb := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: "kueue-validating-admission-policy-binding",
			},
		}

		// Create a DSCI to provide the application namespace
		dsci := &unstructured.Unstructured{}
		dsci.SetGroupVersionKind(gvk.DSCInitialization)
		dsci.SetName("test-dsci")
		dsci.SetNamespace("test-namespace")
		err := unstructured.SetNestedField(dsci.Object, "test-app-ns", "spec", "applicationsNamespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		cli, err := fakeclient.New(fakeclient.WithObjects(vapb, dsci))
		g.Expect(err).ShouldNot(HaveOccurred())

		// Call CleanupExistingResource which should trigger the Kueue VAPB cleanup
		err = upgrade.CleanupExistingResource(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Verify that the ValidatingAdmissionPolicyBinding was deleted
		var deletedVAPB admissionregistrationv1.ValidatingAdmissionPolicyBinding
		err = cli.Get(ctx, client.ObjectKey{Name: "kueue-validating-admission-policy-binding"}, &deletedVAPB)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("not found"))
	})

	t.Run("should handle NotFound error gracefully during upgrade deletion", func(t *testing.T) {
		g := NewWithT(t)

		// Create a DSCI to provide the application namespace
		dsci := &unstructured.Unstructured{}
		dsci.SetGroupVersionKind(gvk.DSCInitialization)
		dsci.SetName("test-dsci")
		dsci.SetNamespace("test-namespace")
		err := unstructured.SetNestedField(dsci.Object, "test-app-ns", "spec", "applicationsNamespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		interceptorFuncs := interceptor.Funcs{
			Delete: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				return k8serr.NewNotFound(schema.GroupResource{
					Group:    "admissionregistration.k8s.io",
					Resource: "validatingadmissionpolicybindings",
				}, "kueue-validating-admission-policy-binding")
			},
		}

		cli, err := fakeclient.New(
			fakeclient.WithObjects(dsci),
			fakeclient.WithInterceptorFuncs(interceptorFuncs),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Call CleanupExistingResource when the VAPB doesn't exist (NotFound error)
		err = upgrade.CleanupExistingResource(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred(), "Should handle NotFound error gracefully")
	})

	t.Run("should handle NoMatch API error gracefully during upgrade deletion", func(t *testing.T) {
		g := NewWithT(t)

		// Create a DSCI to provide the application namespace
		dsci := &unstructured.Unstructured{}
		dsci.SetGroupVersionKind(gvk.DSCInitialization)
		dsci.SetName("test-dsci")
		dsci.SetNamespace("test-namespace")
		err := unstructured.SetNestedField(dsci.Object, "test-app-ns", "spec", "applicationsNamespace")
		g.Expect(err).ShouldNot(HaveOccurred())

		interceptorFuncs := interceptor.Funcs{
			Delete: func(ctx context.Context, client client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				return &meta.NoKindMatchError{
					GroupKind: schema.GroupKind{
						Group: "admissionregistration.k8s.io",
						Kind:  "ValidatingAdmissionPolicyBinding",
					},
					SearchedVersions: []string{"v1beta1"},
				}
			},
		}

		cli, err := fakeclient.New(
			fakeclient.WithObjects(dsci),
			fakeclient.WithInterceptorFuncs(interceptorFuncs),
		)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Call CleanupExistingResource when the VAPB API v1beta1 is not available (NoMatch error)
		err = upgrade.CleanupExistingResource(ctx, cli)
		g.Expect(err).ShouldNot(HaveOccurred(), "Should handle NoMatch error gracefully")
	})
}
