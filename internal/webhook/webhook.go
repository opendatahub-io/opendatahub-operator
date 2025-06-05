//go:build !nowebhook

package webhook

import (
	ctrl "sigs.k8s.io/controller-runtime"

	dscwebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster"
	dsciwebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization"
)

// RegisterAllWebhooks registers all webhook setup functions with the given manager.
// Returns the first error encountered during registration, or nil if all succeed.
func RegisterAllWebhooks(mgr ctrl.Manager) error {
	webhookRegistrations := []func(ctrl.Manager) error{
		dscwebhook.RegisterWebhooks,
		dsciwebhook.RegisterWebhooks,
	}
	for _, reg := range webhookRegistrations {
		if err := reg(mgr); err != nil {
			return err
		}
	}
	return nil
}
