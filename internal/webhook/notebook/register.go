//go:build !nowebhook

package notebook

import (
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// RegisterWebhooks registers the combined connection webhook that handles both validation and mutation for notebooks.
func RegisterWebhooks(mgr ctrl.Manager) error {
	if err := (&NotebookWebhook{
		Client:    mgr.GetClient(),
		APIReader: mgr.GetAPIReader(),
		Decoder:   admission.NewDecoder(mgr.GetScheme()),
		Name:      "notebook-webhook",
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
