//go:build !nowebhook

package dscinitialization

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

// RegisterWebhooks registers the webhooks for DSCInitialization.
func RegisterWebhooks(mgr ctrl.Manager) error {
	if err := (&Validator{
		Client: mgr.GetAPIReader(),
		Name:   "dscinitialization-validating",
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
