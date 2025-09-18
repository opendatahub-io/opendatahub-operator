//go:build !nowebhook

package v1

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

// RegisterWebhooks registers the webhooks for DSCInitialization v1.
func RegisterWebhooks(mgr ctrl.Manager) error {
	if err := (&Validator{
		Client: mgr.GetAPIReader(),
		Name:   "dscinitialization-v1-validating",
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
