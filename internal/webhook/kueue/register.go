//go:build !nowebhook

package kueue

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

// RegisterWebhooks registers the webhooks for kueue label validation.
//
// Parameters:
//   - mgr: The controller-runtime manager to register webhooks with.
//
// Returns:
//   - error: Any error encountered during webhook registration.
func RegisterWebhooks(_ ctrl.Manager) error {
	// NOTE: kueue validating webhook is disabled. To re-enable, uncomment the
	// lines below and restore the +kubebuilder:webhook: markers in validating.go.
	//
	// if err := (&Validator{
	// 	Client:  mgr.GetAPIReader(),
	// 	Decoder: admission.NewDecoder(mgr.GetScheme()),
	// 	Name:    "kueue-validating",
	// }).SetupWithManager(mgr); err != nil {
	// 	return err
	// }

	return nil
}
