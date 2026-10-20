//nolint:testpackage // Exercises package-private webhook namespace resolution.
package modules

import (
	"context"
	"strings"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
)

func collectorWebhook(name, namespace string) *admissionregistrationv1.ValidatingWebhookConfiguration {
	return &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{{
			Name: "vopentelemetrycollector.kb.io",
			ClientConfig: admissionregistrationv1.WebhookClientConfig{Service: &admissionregistrationv1.ServiceReference{
				Name: "opentelemetry-operator-webhook-service", Namespace: namespace,
			}},
			Rules: []admissionregistrationv1.RuleWithOperations{{
				Rule: admissionregistrationv1.Rule{APIGroups: []string{"opentelemetry.io"}, Resources: []string{"opentelemetrycollectors"}},
			}},
		}},
	}
}

func TestResolveOTelOperatorNamespace(t *testing.T) {
	unrelated := collectorWebhook("other", "wrong")
	unrelated.Webhooks[0].ClientConfig.Service.Name = "other-service"
	for _, tc := range []struct {
		name, want, wantError string
		objects               []client.Object
	}{
		{name: "not installed"},
		{
			name: "active webhook", want: "openshift-operators",
			objects: []client.Object{collectorWebhook("collector-create", "openshift-operators"), collectorWebhook("collector-delete", "openshift-operators")},
		},
		{
			name: "conflicting installations", wantError: "multiple namespaces",
			objects: []client.Object{collectorWebhook("collector-one", "openshift-operators"), collectorWebhook("collector-two", "other-operators")},
		},
		{name: "unrelated webhook", objects: []client.Object{unrelated}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cli, err := fakeclient.New(fakeclient.WithObjects(tc.objects...))
			if err != nil {
				t.Fatal(err)
			}
			got, err := resolveOTelOperatorNamespace(context.Background(), cli)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("expected %q error, got %v", tc.wantError, err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("namespace: want %q, got %q (error: %v)", tc.want, got, err)
			}
		})
	}
}
