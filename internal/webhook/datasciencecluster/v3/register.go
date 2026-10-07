//go:build !nowebhook

package v3

import (
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// RegisterWebhooks registers admission webhooks for DataScienceCluster v3.
func RegisterWebhooks(mgr ctrl.Manager) error {
	if err := (&Validator{
		Client:  mgr.GetAPIReader(),
		Name:    "datasciencecluster-v3-validating",
		Decoder: admission.NewDecoder(mgr.GetScheme()),
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	if err := (&Defaulter{
		Name: "datasciencecluster-v3-defaulter",
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
