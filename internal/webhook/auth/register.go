//go:build !nowebhook

package auth

import (
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// RegisterWebhooks registers all Auth-related webhooks with the given manager.
// Returns the first error encountered during registration, or nil if all succeed.
func RegisterWebhooks(mgr ctrl.Manager) error {
	if err := (&Validator{
		Client:  mgr.GetClient(),
		Decoder: admission.NewDecoder(mgr.GetScheme()),
		Name:    "auth-validator",
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
