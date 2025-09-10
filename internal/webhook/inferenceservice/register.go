//go:build !nowebhook

package inferenceservice

import (
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// RegisterWebhooks registers the combined connection webhook that handles both validation and mutation.
func RegisterWebhooks(mgr ctrl.Manager) error {
	if err := (&ConnectionWebhook{
		APIReader: mgr.GetAPIReader(),
		Client:    mgr.GetClient(),
		Decoder:   admission.NewDecoder(mgr.GetScheme()),
		Name:      "connection-isvc",
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
