//go:build !nowebhook

package v2

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

// RegisterWebhooks registers the webhooks for DataScienceCluster v2.
func RegisterWebhooks(mgr ctrl.Manager) error {
	// Register the validating webhook
	if err := (&Validator{
		Client: mgr.GetAPIReader(),
		Name:   "datasciencecluster-v2-validating",
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	// Register the defaulting webhook
	if err := (&Defaulter{
		Name: "datasciencecluster-v2-defaulter",
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
