package e2e_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResolveActiveOpenTelemetryOperatorNamespace(t *testing.T) {
	testScheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(testScheme))

	t.Run("resolves the namespace of the active Collector webhook", func(t *testing.T) {
		cli := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(
			otelCollectorWebhookConfiguration("inactive-copy", "other-namespace", "wrong-service"),
			otelCollectorWebhookConfiguration("active", "openshift-opentelemetry-operator", "opentelemetry-operator-webhook-service"),
		).Build()

		namespace, err := resolveActiveOpenTelemetryOperatorNamespace(context.Background(), cli)
		require.NoError(t, err)
		require.Equal(t, "openshift-opentelemetry-operator", namespace)
	})

	t.Run("returns an error when Collector webhooks target multiple namespaces", func(t *testing.T) {
		cli := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(
			otelCollectorWebhookConfiguration("one", "openshift-opentelemetry-operator", "opentelemetry-operator-webhook-service"),
			otelCollectorWebhookConfiguration("two", "openshift-operators", "opentelemetry-operator-webhook-service"),
		).Build()

		_, err := resolveActiveOpenTelemetryOperatorNamespace(context.Background(), cli)
		require.EqualError(t, err, "OpenTelemetry Collector admission webhooks target multiple namespaces: openshift-opentelemetry-operator, openshift-operators")
	})

	t.Run("returns an error when no active Collector webhook exists", func(t *testing.T) {
		cli := fake.NewClientBuilder().WithScheme(testScheme).WithObjects(
			otelCollectorWebhookConfiguration("inactive-copy", "other-namespace", "wrong-service"),
		).Build()

		_, err := resolveActiveOpenTelemetryOperatorNamespace(context.Background(), cli)
		require.EqualError(t, err, "no active OpenTelemetry Collector admission webhook was found")
	})
}

func otelCollectorWebhookConfiguration(name, namespace, serviceName string) client.Object {
	return &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{{
			ClientConfig: admissionregistrationv1.WebhookClientConfig{
				Service: &admissionregistrationv1.ServiceReference{
					Namespace: namespace,
					Name:      serviceName,
				},
			},
			Rules: []admissionregistrationv1.RuleWithOperations{{
				Rule: admissionregistrationv1.Rule{
					APIGroups: []string{"opentelemetry.io"},
					Resources: []string{"opentelemetrycollectors"},
				},
			}},
		}},
	}
}
