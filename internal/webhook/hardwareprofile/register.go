//go:build !nowebhook

package hardwareprofile

import (
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// RegisterWebhooks registers the webhooks for hardware profile injection.
//
// Parameters:
//   - mgr: The controller-runtime manager to register webhooks with.
//
// Returns:
//   - error: Any error encountered during webhook registration.
func RegisterWebhooks(mgr ctrl.Manager) error {
	if err := (&Injector{
		Client:  mgr.GetAPIReader(),
		Decoder: admission.NewDecoder(mgr.GetScheme()),
		Name:    "hardwareprofile-injector",
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
