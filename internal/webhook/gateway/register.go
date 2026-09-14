//go:build !nowebhook

package gateway

import (
	"errors"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// RegisterWebhooks registers the GatewayConfig validating webhook.
func RegisterWebhooks(mgr ctrl.Manager) error {
	if mgr == nil {
		return errors.New("manager cannot be nil")
	}

	return (&Validator{
		Decoder: admission.NewDecoder(mgr.GetScheme()),
		Name:    "gatewayconfig-validating",
	}).SetupWithManager(mgr)
}
