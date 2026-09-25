//go:build !nowebhook

package v1alpha1

import (
	ctrl "sigs.k8s.io/controller-runtime"

	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
)

// RegisterWebhooks registers the Platform v1alpha1 conversion webhook.
func RegisterWebhooks(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &configv1alpha1.Platform{}).Complete()
}
