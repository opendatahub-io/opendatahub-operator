//go:build !nowebhook

package datasciencecluster

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

// RegisterWebhooks registers the webhooks for DataScienceCluster.
func RegisterWebhooks(mgr ctrl.Manager) error {
	// Register the validating webhook
	if err := (&Validator{
		Client: mgr.GetAPIReader(),
		Name:   "datasciencecluster-validating",
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	// Register the defaulting webhook
	if err := (&Defaulter{
		Name: "datasciencecluster-defaulter",
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
