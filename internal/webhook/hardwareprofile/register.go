//go:build !nowebhook

package hardwareprofile

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

// RegisterWebhooks registers the webhooks for hardware profile injection.
//
// Parameters:
//   - mgr: The controller-runtime manager to register webhooks with.
//
// Returns:
//   - error: Any error encountered during webhook registration.
func RegisterWebhooks(mgr ctrl.Manager) error {
	// Register the mutating webhook
	if err := (&Injector{
		Client: mgr.GetAPIReader(),
		Name:   "hardwareprofile-injector",
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
