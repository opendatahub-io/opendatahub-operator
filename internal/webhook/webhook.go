//go:build !nowebhook

package webhook

import (
	ctrl "sigs.k8s.io/controller-runtime"

	dscwebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster"
	dsciwebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization"
	hardwareprofilewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/hardwareprofile"
	isvc "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/inferenceservice"
	kueuewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/kueue"
	notebookwebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/notebook"
)

// RegisterAllWebhooks registers all webhook setup functions with the given manager.
// Returns the first error encountered during registration, or nil if all succeed.
func RegisterAllWebhooks(mgr ctrl.Manager) error {
	webhookRegistrations := []func(ctrl.Manager) error{
		dscwebhook.RegisterWebhooks,
		dsciwebhook.RegisterWebhooks,
		hardwareprofilewebhook.RegisterWebhooks,
		kueuewebhook.RegisterWebhooks,
		isvc.RegisterWebhooks,
		notebookwebhook.RegisterWebhooks,
	}
	for _, reg := range webhookRegistrations {
		if err := reg(mgr); err != nil {
			return err
		}
	}
	return nil
}
