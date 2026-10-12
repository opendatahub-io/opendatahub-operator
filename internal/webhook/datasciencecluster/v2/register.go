//go:build !nowebhook

package v2

import (
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
)

// RegisterWebhooks registers the webhooks for DataScienceCluster v2.
func RegisterWebhooks(mgr ctrl.Manager) error {
	if err := ctrl.NewWebhookManagedBy(mgr, &dscv2.DataScienceCluster{}).Complete(); err != nil {
		return err
	}

	// Register the validating webhook
	if err := (&Validator{
		Client:  mgr.GetAPIReader(),
		Name:    "datasciencecluster-v2-validating",
		Decoder: admission.NewDecoder(mgr.GetScheme()),
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
