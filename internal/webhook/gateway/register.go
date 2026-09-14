//go:build !nowebhook

package gateway

import (
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// RegisterWebhooks registers the GatewayConfig validating webhook.
func RegisterWebhooks(mgr ctrl.Manager) error {
	return (&Validator{
		Decoder: admission.NewDecoder(mgr.GetScheme()),
		Name:    "gatewayconfig-validating",
	}).SetupWithManager(mgr)
}
