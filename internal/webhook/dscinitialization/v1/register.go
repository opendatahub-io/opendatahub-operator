//go:build !nowebhook

package v1

import (
	ctrl "sigs.k8s.io/controller-runtime"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
)

// RegisterWebhooks registers the webhooks for DSCInitialization v1.
func RegisterWebhooks(mgr ctrl.Manager) error {
	// Register the conversion webhook
	if err := ctrl.NewWebhookManagedBy(mgr).For(&dsciv1.DSCInitialization{}).Complete(); err != nil {
		return err
	}

	if err := (&Validator{
		Client: mgr.GetAPIReader(),
		Name:   "dscinitialization-v1-validating",
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
