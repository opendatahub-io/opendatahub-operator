//go:build !nowebhook

package kueue

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

// RegisterWebhooks registers the webhooks for kueue.
func RegisterWebhooks(mgr ctrl.Manager) error {
	if err := (&Validator{
		Client: mgr.GetClient(),
		Name:   "kueue-validating",
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
