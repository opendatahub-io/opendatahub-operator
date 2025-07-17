package webhook_test

import (
	"context"
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

// FakePatchClient converts Patch(Apply) into Create for fake client testing.
type FakePatchClient struct {
	client.Client
}

func (f *FakePatchClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return f.Create(ctx, obj)
}

func Test_ReconcileWebhooks(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	ctx := context.Background()
	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(admissionregistrationv1.AddToScheme(scheme)).To(Succeed())

	baseClient := fake.NewClientBuilder().WithScheme(scheme).Build()
	fakeClient := &FakePatchClient{Client: baseClient}

	// Seed a source ValidatingWebhookConfiguration
	sourceVWC := &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sample-webhook.opendatahub.io",
		},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{
			{
				Name: "sample-webhook",
				ClientConfig: admissionregistrationv1.WebhookClientConfig{
					CABundle: []byte("dummy-ca-bundle"),
					Service: &admissionregistrationv1.ServiceReference{
						Name:      "dummy-service",
						Namespace: "dummy-namespace",
						Path:      strPtr("/dummy-path"),
						Port:      int32Ptr(443),
					},
				},
			},
		},
	}
	g.Expect(fakeClient.Create(ctx, sourceVWC)).To(Succeed())

	// Create owner Namespace
	owner := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-operator-ns",
			UID:  types.UID("owner-uid-1234"),
		},
	}
	g.Expect(fakeClient.Create(ctx, owner)).To(Succeed())

	result, err := webhook.ReconcileWebhooks(ctx, fakeClient, scheme, owner)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(result.Requeue).To(BeFalse())

	vwc := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	g.Expect(fakeClient.Get(ctx, types.NamespacedName{
		Name: webhook.ValidatingWebhookConfigName,
	}, vwc)).To(Succeed())

	t.Run("HasInjectCabundleAnnotation", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)
		g.Expect(vwc.Annotations).To(HaveKeyWithValue(webhook.InjectCabundleAnnotation, "true"))
	})

	t.Run("HasCorrectOwnerReference", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)
		var found bool
		for _, ref := range vwc.OwnerReferences {
			if ref.Kind == "Namespace" && ref.Name == "test-operator-ns" {
				found = true
				break
			}
		}
		g.Expect(found).To(BeTrue(), "OwnerReference should match owner namespace")
	})

	t.Run("WebhooksHaveCorrectClientConfig", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)
		wh := vwc.Webhooks[0]
		g.Expect(wh.ClientConfig.CABundle).To(Equal([]byte("dummy-ca-bundle")))
		g.Expect(wh.ClientConfig.Service).ToNot(BeNil())
		g.Expect(wh.ClientConfig.Service.Name).To(Equal("dummy-service"))
		g.Expect(wh.ClientConfig.Service.Namespace).To(Equal("dummy-namespace"))
		g.Expect(wh.ClientConfig.Service.Port).To(Equal(int32Ptr(443)))
		g.Expect(wh.ClientConfig.Service.Path).ToNot(BeNil())
		g.Expect(*wh.ClientConfig.Service.Path).To(Equal("/validate-kueue"))
	})

	t.Run("HasExpectedWebhookNames", func(t *testing.T) {
		t.Parallel()
		g := NewWithT(t)
		expectedNames := map[string]struct{}{
			webhook.KserveKueuelabelsValidatorName:               {},
			webhook.KubeflowKueuelabelsValidatorName:             {},
			webhook.RayKueuelabelsValidatorName:                  {},
			webhook.KserveKueuelabelsValidatorName + "-legacy":   {},
			webhook.KubeflowKueuelabelsValidatorName + "-legacy": {},
			webhook.RayKueuelabelsValidatorName + "-legacy":      {},
		}
		for _, wh := range vwc.Webhooks {
			_, ok := expectedNames[wh.Name]
			g.Expect(ok).To(BeTrue(), "Unexpected webhook name: %s", wh.Name)
		}
	})
}

func strPtr(s string) *string { return &s }
func int32Ptr(i int32) *int32 { return &i }
