//go:build !nowebhook

package v1

import (
	ctrl "sigs.k8s.io/controller-runtime"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
)

// RegisterWebhooks registers the webhooks for DataScienceCluster v1.
func RegisterWebhooks(mgr ctrl.Manager) error {
	// Register the conversion webhook
	if err := ctrl.NewWebhookManagedBy(mgr).For(&dscv1.DataScienceCluster{}).Complete(); err != nil {
		return err
	}

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
