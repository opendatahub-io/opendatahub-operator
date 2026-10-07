//nolint:testpackage // These tests exercise the retry helper without starting a full envtest control plane.
package envt

import (
	"context"
	"errors"
	"testing"

	"github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsfake "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/fake"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ktesting "k8s.io/client-go/testing"
)

func TestConfigureCRDConversionRetriesConflict(t *testing.T) {
	g := gomega.NewWithT(t)
	const crdName = "test.example.com"
	crd := &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name:   crdName,
			Labels: map[string]string{"preserved": "true"},
		},
	}
	client := apiextensionsfake.NewSimpleClientset(crd)
	updateCalls := 0
	client.PrependReactor("update", "customresourcedefinitions", func(action ktesting.Action) (bool, runtime.Object, error) {
		updateCalls++
		if updateCalls == 1 {
			return true, nil, k8serr.NewConflict(
				schema.GroupResource{Group: apiextensionsv1.GroupName, Resource: "customresourcedefinitions"},
				crdName,
				errors.New("stale resource version"),
			)
		}
		return false, nil, nil
	})

	conversion := &apiextensionsv1.CustomResourceConversion{
		Strategy: apiextensionsv1.WebhookConverter,
		Webhook: &apiextensionsv1.WebhookConversion{
			ConversionReviewVersions: []string{"v1"},
		},
	}
	err := configureCRDConversion(context.Background(), client.ApiextensionsV1().CustomResourceDefinitions(), crdName, conversion)

	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(updateCalls).To(gomega.Equal(2))
	g.Expect(client.Actions()).To(gomega.HaveLen(4)) // a GET and update for each attempt

	updated, err := client.ApiextensionsV1().CustomResourceDefinitions().Get(context.Background(), crdName, metav1.GetOptions{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(updated.Labels).To(gomega.HaveKeyWithValue("preserved", "true"))
	g.Expect(updated.Spec.Conversion).To(gomega.Equal(conversion))
}

func TestConfigureCRDConversionReturnsContextAfterConflictsExhausted(t *testing.T) {
	g := gomega.NewWithT(t)
	const crdName = "test.example.com"
	client := apiextensionsfake.NewSimpleClientset(&apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: crdName},
	})
	updateCalls := 0
	client.PrependReactor("update", "customresourcedefinitions", func(action ktesting.Action) (bool, runtime.Object, error) {
		updateCalls++
		return true, nil, k8serr.NewConflict(
			schema.GroupResource{Group: apiextensionsv1.GroupName, Resource: "customresourcedefinitions"},
			crdName,
			errors.New("stale resource version"),
		)
	})

	err := configureCRDConversion(context.Background(), client.ApiextensionsV1().CustomResourceDefinitions(), crdName, &apiextensionsv1.CustomResourceConversion{})

	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(k8serr.IsConflict(err)).To(gomega.BeTrue())
	g.Expect(err.Error()).To(gomega.ContainSubstring(crdName))
	g.Expect(err.Error()).To(gomega.ContainSubstring("configure conversion"))
	g.Expect(updateCalls).To(gomega.BeNumerically(">", 1))
}
