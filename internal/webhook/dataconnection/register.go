//go:build !nowebhook

package dataconnection

import (
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// RegisterWebhooks registers the combined data connection webhook that handles both validation and mutation.
func RegisterWebhooks(mgr ctrl.Manager) error {
	if err := (&DataConnectionWebhook{
		Client:  mgr.GetAPIReader(),
		Decoder: admission.NewDecoder(mgr.GetScheme()),
		Name:    "dataconnection-webhook",
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
