//nolint:testpackage // testing unexported methods
package common

import (
	"context"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"

	. "github.com/onsi/gomega"
)

const (
	testCRDGroup     = "test.example.com"
	testCRDVersion   = "v1"
	testCRDKind      = "Widget"
	testCRDComponent = "test"
)

// TODO(OSSM-12397): Remove this test once the sail-operator ships a fix.
func TestAnnotateIstioWebhooksHook(t *testing.T) {
	t.Run("should annotate webhooks when they exist without annotation", func(t *testing.T) {
		g := NewWithT(t)

		mutatingWH := &admissionregistrationv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: istioSidecarInjectorWebhook,
			},
		}
		validatingWH := &admissionregistrationv1.ValidatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: istioValidatorWebhook,
			},
		}

		cli, err := fakeclient.New(fakeclient.WithObjects(mutatingWH, validatingWH))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{Client: cli}

		hook := AnnotateIstioWebhooksHook()
		err = hook(context.Background(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		var updatedMutating admissionregistrationv1.MutatingWebhookConfiguration
		err = cli.Get(context.Background(), types.NamespacedName{Name: istioSidecarInjectorWebhook}, &updatedMutating)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedMutating.Annotations[sailOperatorIgnoreAnnotation]).Should(Equal("true"))

		var updatedValidating admissionregistrationv1.ValidatingWebhookConfiguration
		err = cli.Get(context.Background(), types.NamespacedName{Name: istioValidatorWebhook}, &updatedValidating)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedValidating.Annotations[sailOperatorIgnoreAnnotation]).Should(Equal("true"))
	})

	t.Run("should be a no-op when webhooks already have the annotation", func(t *testing.T) {
		g := NewWithT(t)

		mutatingWH := &admissionregistrationv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: istioSidecarInjectorWebhook,
				Annotations: map[string]string{
					sailOperatorIgnoreAnnotation: "true",
				},
			},
		}
		validatingWH := &admissionregistrationv1.ValidatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: istioValidatorWebhook,
				Annotations: map[string]string{
					sailOperatorIgnoreAnnotation: "true",
				},
			},
		}

		cli, err := fakeclient.New(fakeclient.WithObjects(mutatingWH, validatingWH))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{Client: cli}

		hook := AnnotateIstioWebhooksHook()
		err = hook(context.Background(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		var updatedMutating admissionregistrationv1.MutatingWebhookConfiguration
		err = cli.Get(context.Background(), types.NamespacedName{Name: istioSidecarInjectorWebhook}, &updatedMutating)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedMutating.Annotations[sailOperatorIgnoreAnnotation]).Should(Equal("true"))
	})

	t.Run("should be a no-op when webhooks do not exist", func(t *testing.T) {
		g := NewWithT(t)

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{Client: cli}

		hook := AnnotateIstioWebhooksHook()
		err = hook(context.Background(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should preserve existing annotations when adding the ignore annotation", func(t *testing.T) {
		g := NewWithT(t)

		mutatingWH := &admissionregistrationv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: istioSidecarInjectorWebhook,
				Annotations: map[string]string{
					"existing-annotation": "existing-value",
				},
			},
		}

		cli, err := fakeclient.New(fakeclient.WithObjects(mutatingWH))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{Client: cli}

		hook := AnnotateIstioWebhooksHook()
		err = hook(context.Background(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		var updatedMutating admissionregistrationv1.MutatingWebhookConfiguration
		err = cli.Get(context.Background(), types.NamespacedName{Name: istioSidecarInjectorWebhook}, &updatedMutating)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(updatedMutating.Annotations[sailOperatorIgnoreAnnotation]).Should(Equal("true"))
		g.Expect(updatedMutating.Annotations["existing-annotation"]).Should(Equal("existing-value"))
	})
}

func TestSkipCRDIfPresent(t *testing.T) {
	// Use a fake CRD name unrelated to any real resource.
	// mocks.NewMockCRD generates the name as "<plural>.<group>".
	testCRDName := mocks.NewMockCRD(testCRDGroup, testCRDVersion, testCRDKind, testCRDComponent).Name

	t.Run("should keep resources when CRD does not exist in cluster", func(t *testing.T) {
		g := NewWithT(t)

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{Client: cli}
		rr.Resources = newUnstructuredResources(t, cli)
		initialCount := len(rr.Resources)

		hook := SkipCRDIfPresent(testCRDName)
		err = hook(context.Background(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(rr.Resources).Should(HaveLen(initialCount))
	})

	t.Run("should remove CRD from resources when it exists without infrastructure label", func(t *testing.T) {
		g := NewWithT(t)

		crd := mocks.NewMockCRD(testCRDGroup, testCRDVersion, testCRDKind, testCRDComponent)

		cli, err := fakeclient.New(fakeclient.WithObjects(crd))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{Client: cli}
		rr.Resources = newUnstructuredResources(t, cli)
		initialCount := len(rr.Resources)

		hook := SkipCRDIfPresent(testCRDName)
		err = hook(context.Background(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(rr.Resources).Should(HaveLen(initialCount - 1))

		for _, res := range rr.Resources {
			if res.GetKind() == gvk.CustomResourceDefinition.Kind {
				g.Expect(res.GetName()).ShouldNot(Equal(testCRDName))
			}
		}
	})

	t.Run("should keep resources when CRD exists with infrastructure label", func(t *testing.T) {
		g := NewWithT(t)

		crd := mocks.NewMockCRD(testCRDGroup, testCRDVersion, testCRDKind, testCRDComponent)
		crd.Labels[labels.InfrastructurePartOf] = "test-component"

		cli, err := fakeclient.New(fakeclient.WithObjects(crd))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &odhtypes.ReconciliationRequest{Client: cli}
		rr.Resources = newUnstructuredResources(t, cli)
		initialCount := len(rr.Resources)

		hook := SkipCRDIfPresent(testCRDName)
		err = hook(context.Background(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(rr.Resources).Should(HaveLen(initialCount))
	})
}

// newUnstructuredResources creates a slice of unstructured resources containing
// a CRD and a ConfigMap for use in hook tests.
func newUnstructuredResources(t *testing.T, cli client.Client) []unstructured.Unstructured {
	t.Helper()

	g := NewWithT(t)

	rr := &odhtypes.ReconciliationRequest{Client: cli}

	err := rr.AddResources(
		mocks.NewMockCRD(testCRDGroup, testCRDVersion, testCRDKind, testCRDComponent),
		mocks.NewMockCRD("other.example.com", "v1", "Gadget", "other"),
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-configmap",
				Namespace: "default",
			},
		},
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	return rr.Resources
}
