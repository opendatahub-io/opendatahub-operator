//nolint:testpackage // Exercises package-private webhook namespace resolution.
package modules

import (
	"context"
	"strings"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
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

func TestOTelCollectorWebhookWatch(t *testing.T) {
	matching := collectorWebhook("collector-admission", "openshift-operators")
	unrelated := collectorWebhook("other", "wrong")
	unrelated.Webhooks[0].ClientConfig.Service.Name = "other-service"
	predicate := otelCollectorWebhookPredicate()

	tests := []struct {
		name string
		run  func() bool
		want bool
	}{
		{
			name: "matching create",
			run:  func() bool { return predicate.Create(event.CreateEvent{Object: matching}) },
			want: true,
		},
		{
			name: "unrelated create",
			run:  func() bool { return predicate.Create(event.CreateEvent{Object: unrelated}) },
		},
		{
			name: "matching delete",
			run:  func() bool { return predicate.Delete(event.DeleteEvent{Object: matching}) },
			want: true,
		},
		{
			name: "unrelated delete",
			run:  func() bool { return predicate.Delete(event.DeleteEvent{Object: unrelated}) },
		},
		{
			name: "update changes target namespace",
			run: func() bool {
				return predicate.Update(event.UpdateEvent{
					ObjectOld: collectorWebhook("collector-admission", "old-operators"),
					ObjectNew: collectorWebhook("collector-admission", "new-operators"),
				})
			},
			want: true,
		},
		{
			name: "unrelated update",
			run: func() bool {
				return predicate.Update(event.UpdateEvent{
					ObjectOld: unrelated.DeepCopy(),
					ObjectNew: unrelated.DeepCopy(),
				})
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.run(); got != tc.want {
				t.Fatalf("predicate: want %t, got %t", tc.want, got)
			}
		})
	}

	requests := otelCollectorWebhookPlatformRequests(context.Background(), matching)
	want := []reconcile.Request{{NamespacedName: k8stypes.NamespacedName{Name: configApi.PlatformInstanceName}}}
	if len(requests) != 1 || requests[0] != want[0] {
		t.Fatalf("requests: want %v, got %v", want, requests)
	}
}
