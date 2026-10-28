package modules

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
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

	if len(namespaces) == 0 {
		return "", nil // OTel may not be installed yet.
	}
	if len(namespaces) > 1 {
		found := make([]string, 0, len(namespaces))
		for namespace := range namespaces {
			found = append(found, namespace)
		}
		sort.Strings(found)
		return "", fmt.Errorf("collector admission webhooks target multiple namespaces: %s", strings.Join(found, ", "))
	}
	for namespace := range namespaces {
		return namespace, nil
	}
	return "", nil
}

func isOTelCollectorWebhook(webhook admissionregistrationv1.ValidatingWebhook) bool {
	service := webhook.ClientConfig.Service
	if service == nil || service.Namespace == "" || service.Name != "opentelemetry-operator-webhook-service" {
		return false
	}
	for _, rule := range webhook.Rules {
		if (slices.Contains(rule.APIGroups, "opentelemetry.io") || slices.Contains(rule.APIGroups, "*")) &&
			(slices.Contains(rule.Resources, "opentelemetrycollectors") || slices.Contains(rule.Resources, "*")) {
			return true
		}
	}
	return false
}

func isOTelCollectorWebhookConfiguration(obj client.Object) bool {
	configuration, ok := obj.(*admissionregistrationv1.ValidatingWebhookConfiguration)
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

func otelCollectorWebhookPlatformRequests(_ context.Context, _ client.Object) []reconcile.Request {
	return []reconcile.Request{{NamespacedName: k8stypes.NamespacedName{Name: configApi.PlatformInstanceName}}}
}
