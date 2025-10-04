//go:build !nowebhook

package dashboard

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

// RegisterWebhooks registers all dashboard deprecation webhooks.
func RegisterWebhooks(mgr ctrl.Manager) error {
	if err := RegisterAcceleratorProfileWebhook(mgr); err != nil {
		return err
	}

	if err := RegisterHardwareProfileWebhook(mgr); err != nil {
		return err
	}

	return nil
}
