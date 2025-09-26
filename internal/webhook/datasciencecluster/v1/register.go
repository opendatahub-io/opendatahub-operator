//go:build !nowebhook

package v1

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

// RegisterWebhooks registers the webhooks for DataScienceCluster v1.
func RegisterWebhooks(mgr ctrl.Manager) error {
	// Register the validating webhook
	if err := (&Validator{
		Client: mgr.GetAPIReader(),
		Name:   "datasciencecluster-v1-validating",
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	// Register the defaulting webhook
	if err := (&Defaulter{
		Name: "datasciencecluster-v1-defaulter",
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
