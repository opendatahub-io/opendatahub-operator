//go:build !nowebhook

package kueue

import (
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// RegisterWebhooks registers the webhooks for kueue label validation.
//
// Parameters:
//   - mgr: The controller-runtime manager to register webhooks with.
//
// Returns:
//   - error: Any error encountered during webhook registration.
func RegisterWebhooks(mgr ctrl.Manager) error {
	if err := (&Validator{
		Client:  mgr.GetAPIReader(),
		Decoder: admission.NewDecoder(mgr.GetScheme()),
		Name:    "kueue-validating",
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
