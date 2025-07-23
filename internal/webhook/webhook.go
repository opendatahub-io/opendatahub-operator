//go:build !nowebhook

package webhook

import (
	ctrl "sigs.k8s.io/controller-runtime"

	authwebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/auth"
	dc "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dataconnection"
	dscwebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster"
	dsciwebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization"
	hardwareprofilewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/hardwareprofile"
	kueuewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/kueue"
)

// RegisterAllWebhooks registers all webhook setup functions with the given manager.
// Returns the first error encountered during registration, or nil if all succeed.
func RegisterAllWebhooks(mgr ctrl.Manager) error {
	webhookRegistrations := []func(ctrl.Manager) error{
		dscwebhook.RegisterWebhooks,
		dsciwebhook.RegisterWebhooks,
		authwebhook.RegisterWebhooks,
		hardwareprofilewebhook.RegisterWebhooks,
		kueuewebhook.RegisterWebhooks,
		dc.RegisterWebhooks,
	}
	for _, reg := range webhookRegistrations {
		if err := reg(mgr); err != nil {
			return err
		}
	}
	return nil
}
