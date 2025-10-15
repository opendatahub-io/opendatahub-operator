//go:build !nowebhook

package v2

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

// RegisterWebhooks registers the webhooks for DSCInitialization v2.
func RegisterWebhooks(mgr ctrl.Manager) error {
	if err := (&Validator{
		Client: mgr.GetAPIReader(),
		Name:   "dscinitialization-v2-validating",
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
