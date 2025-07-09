package webhook_test

import (
	"context"
	"os"
	"sort"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook"

	. "github.com/onsi/gomega"
)

// FakePatchClient wraps client.Client to convert Patch(Apply) to Update for tests.
type FakePatchClient struct {
	client.Client
}

func (f *FakePatchClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return f.Create(ctx, obj)
}

func Test_ReconcileWebhooks_CreatesConfigs(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	sch := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(sch)).To(Succeed())
	g.Expect(admissionregistrationv1.AddToScheme(sch)).To(Succeed())

	baseClient := fake.NewClientBuilder().
		WithScheme(sch).
		Build()

	// Fake client does not support Apply, so we need to wrap it
	// https://github.com/kubernetes/kubernetes/issues/115598
	fakeClient := &FakePatchClient{Client: baseClient}
	// Set OPERATOR_NAMESPACE so ReconcileWebhooks picks it up
	const operatorNs = "test-operator-ns"
	t.Setenv("OPERATOR_NAMESPACE", operatorNs)
	defer os.Unsetenv("OPERATOR_NAMESPACE")

	// Create a Namespace object to act as the "owner"
	owner := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: operatorNs,
			UID:  types.UID("owner-uid-1234"),
		},
	}

	// Call ReconcileWebhooks
	err := webhook.ReconcileWebhooks(ctx, fakeClient, sch, owner)
	g.Expect(err).NotTo(HaveOccurred())

	// Fetch the ValidatingWebhookConfiguration
	vwc := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	g.Expect(fakeClient.Get(
		ctx,
		types.NamespacedName{Name: webhook.ValidatingWebhookConfigurationName},
		vwc,
	)).To(Succeed())

	// Both configs must have the inject‑cabundle annotation
	for _, wc := range []metav1.Object{vwc} {
		g.Expect(wc.GetAnnotations()).To(HaveKeyWithValue(
			webhook.InjectCabundleAnnotation, "true",
		))
	}

	// Both configs must carry an ownerRef to Namespace
	expectedOwnerRef := metav1.OwnerReference{
		APIVersion:         corev1.SchemeGroupVersion.String(),
		Kind:               "Namespace",
		Name:               operatorNs,
		UID:                owner.UID,
		Controller:         nil,
		BlockOwnerDeletion: nil,
	}

	for _, wc := range []metav1.Object{vwc} {
		g.Expect(wc.GetOwnerReferences()).To(ContainElement(expectedOwnerRef))
	}

	// Check that the validating config contains exactly the expected names
	expectedValidating := []string{
		webhook.KserveKueuelabelsValidatorName,
		webhook.KubeflowKueuelabelsValidatorName,
		webhook.RayKueuelabelsValidatorName,
		webhook.KserveKueuelabelsValidatorName + "-legacy",
		webhook.KubeflowKueuelabelsValidatorName + "-legacy",
		webhook.RayKueuelabelsValidatorName + "-legacy",
	}
	foundValidating := []string{}
	for _, wh := range vwc.Webhooks {
		foundValidating = append(foundValidating, wh.Name)
	}
	sort.Strings(foundValidating)
	sort.Strings(expectedValidating)
	g.Expect(foundValidating).To(Equal(expectedValidating))

	// Ensure any Kueue‑related validating webhooks have a non‑nil NamespaceSelector
	for _, wh := range vwc.Webhooks {
		if contains(wh.Name, "kueuelabels-validator") {
			g.Expect(wh.NamespaceSelector).NotTo(BeNil(), "expected namespaceSelector on %s", wh.Name)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > len(substr) && (s[:len(substr)] == substr || contains(s[1:], substr)))
}
