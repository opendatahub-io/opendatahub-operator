//go:build !nowebhook

package monitoring

import (
	"errors"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// RegisterWebhooks registers the webhooks for monitoring injection.
//
// Parameters:
//   - mgr: The controller-runtime manager to register webhooks with.
//
// Returns:
//   - error: Any error encountered during webhook registration.
func RegisterWebhooks(mgr ctrl.Manager) error {
	if mgr == nil {
		return errors.New("manager cannot be nil")
	}

	if err := (&Injector{
		Client:  mgr.GetAPIReader(),
		Decoder: admission.NewDecoder(mgr.GetScheme()),
		Name:    "monitoring-injector",
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
