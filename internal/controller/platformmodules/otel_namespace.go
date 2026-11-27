package platformmodules

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	otelCollectorWebhookServiceName = "opentelemetry-operator-webhook-service"
	otelCollectorResourceName       = "opentelemetrycollectors"
)

// The Collector admission webhook is installed by OLM in the namespace where
// the OpenTelemetry Operator runs. CSV copies can appear in other namespaces,
// so the active webhook service is the authoritative source for its namespace.
func resolveOTelOperatorNamespace(ctx context.Context, c client.Client) (string, error) {
	var configurations admissionregistrationv1.ValidatingWebhookConfigurationList
	if err := c.List(ctx, &configurations); err != nil {
		return "", fmt.Errorf("listing validating webhooks: %w", err)
	}

	namespaces := map[string]struct{}{}
	for i := range configurations.Items {
		for _, webhook := range configurations.Items[i].Webhooks {
			if !isOTelCollectorWebhook(webhook) {
				continue
			}
			namespaces[webhook.ClientConfig.Service.Namespace] = struct{}{}
		}
	}

	found := slices.Sorted(maps.Keys(namespaces))
	switch len(found) {
	case 0:
		return "", nil // OTel may not be installed yet.
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("collector admission webhooks target multiple namespaces: %s", strings.Join(found, ", "))
	}
}

func isOTelCollectorWebhook(webhook admissionregistrationv1.ValidatingWebhook) bool {
	service := webhook.ClientConfig.Service
	if service == nil || service.Namespace == "" || service.Name != otelCollectorWebhookServiceName {
		return false
	}
	for _, rule := range webhook.Rules {
		if (slices.Contains(rule.APIGroups, gvk.OpenTelemetryCollector.Group) || slices.Contains(rule.APIGroups, "*")) &&
			(slices.Contains(rule.Resources, otelCollectorResourceName) || slices.Contains(rule.Resources, "*")) {
			return true
		}
	}
	return false
}

// validatingWebhookConfiguration accepts the typed object used in tests and the
// unstructured object the reconciler watch delivers for every event.
func validatingWebhookConfiguration(obj client.Object) (*admissionregistrationv1.ValidatingWebhookConfiguration, bool) {
	if configuration, ok := obj.(*admissionregistrationv1.ValidatingWebhookConfiguration); ok {
		return configuration, true
	}

	unstructuredObj, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return nil, false
	}

	configuration := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, configuration); err != nil {
		return nil, false
	}
	return configuration, true
}

func isOTelCollectorWebhookConfiguration(obj client.Object) bool {
	configuration, ok := validatingWebhookConfiguration(obj)
	if !ok {
		return false
	}
	return slices.ContainsFunc(configuration.Webhooks, isOTelCollectorWebhook)
}

func otelCollectorWebhookPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool { return isOTelCollectorWebhookConfiguration(e.Object) },
		DeleteFunc: func(e event.DeleteEvent) bool { return isOTelCollectorWebhookConfiguration(e.Object) },
		UpdateFunc: func(e event.UpdateEvent) bool {
			return isOTelCollectorWebhookConfiguration(e.ObjectOld) || isOTelCollectorWebhookConfiguration(e.ObjectNew)
		},
	}
}

func monitoringModuleRequests(_ context.Context, _ client.Object) []reconcile.Request {
	return []reconcile.Request{{NamespacedName: k8stypes.NamespacedName{Name: serviceApi.MonitoringServiceName}}}
}
