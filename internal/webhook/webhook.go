//go:build !nowebhook

package webhook

import (
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dashboard"
	dscv1webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v1"
	dscv2webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v2"
	dsciv1webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization/v1"
	dsciv2webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization/v2"
	hardwareprofilewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/hardwareprofile"
	kueuewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/kueue"
	notebookwebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/notebook"
	serving "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/serving"
)

// RegisterAllWebhooks registers all webhook setup functions with the given manager.
// Returns the first error encountered during registration, or nil if all succeed.
func RegisterAllWebhooks(mgr ctrl.Manager) error {
	webhookRegistrations := []func(ctrl.Manager) error{
		dscv1webhook.RegisterWebhooks,
		dscv2webhook.RegisterWebhooks,
		dsciv1webhook.RegisterWebhooks,
		dsciv2webhook.RegisterWebhooks,
		hardwareprofilewebhook.RegisterWebhooks,
		kueuewebhook.RegisterWebhooks,
		serving.RegisterWebhooks,
		notebookwebhook.RegisterWebhooks,
		dashboard.RegisterWebhooks,
	}
	for _, reg := range webhookRegistrations {
		if err := reg(mgr); err != nil {
			return err
		}
	}
	return nil
}
