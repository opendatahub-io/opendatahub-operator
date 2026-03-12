//go:build !nowebhook

package webhook

import (
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	sr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dashboard"
	dscv1webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v1"
	dscv2webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v2"
	dsciv1webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization/v1"
	dsciv2webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/dscinitialization/v2"
	hardwareprofilewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/hardwareprofile"
	monitoringwebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/monitoring"
	notebookwebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/notebook"
	serving "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/serving"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/flags"
)

var log = logf.Log.WithName("webhook-registration")

type webhookEntry struct {
	name     string
	register func(ctrl.Manager) error
	disabled func() bool
}

// RegisterAllWebhooks registers all webhook setup functions with the given manager.
// Webhooks whose suppression flag is set are skipped.
// Returns the first error encountered during registration, or nil if all succeed.
func RegisterAllWebhooks(mgr ctrl.Manager) error {
	entries := []webhookEntry{
		{name: "dsc-v1", register: dscv1webhook.RegisterWebhooks, disabled: func() bool { return !flags.IsDSCEnabled() }},
		{name: "dsc-v2", register: dscv2webhook.RegisterWebhooks, disabled: func() bool { return !flags.IsDSCEnabled() }},
		{name: "dsci-v1", register: dsciv1webhook.RegisterWebhooks, disabled: func() bool { return !flags.IsDSCIEnabled() }},
		{name: "dsci-v2", register: dsciv2webhook.RegisterWebhooks, disabled: func() bool { return !flags.IsDSCIEnabled() }},
		{name: "hardwareprofile", register: hardwareprofilewebhook.RegisterWebhooks, disabled: func() bool {
			return !cr.IsEnabled(componentApi.KserveComponentName) && !cr.IsEnabled(componentApi.WorkbenchesComponentName)
		}},
		// NOTE: kueue validating webhook is disabled. To re-enable, uncomment the entry below.
		// {name: "kueue", register: kueuewebhook.RegisterWebhooks, disabled: func() bool { return !cr.IsEnabled(componentApi.KueueComponentName) }},
		{name: "monitoring", register: monitoringwebhook.RegisterWebhooks, disabled: func() bool { return !sr.IsEnabled(serviceApi.MonitoringServiceName) }},
		{name: "serving", register: serving.RegisterWebhooks, disabled: func() bool { return !cr.IsEnabled(componentApi.KserveComponentName) }},
		{name: "notebook", register: notebookwebhook.RegisterWebhooks, disabled: func() bool { return !cr.IsEnabled(componentApi.WorkbenchesComponentName) }},
		{name: "dashboard", register: dashboard.RegisterWebhooks, disabled: func() bool { return !cr.IsEnabled(componentApi.DashboardComponentName) }},
	}

	for _, e := range entries {
		if e.disabled != nil && e.disabled() {
			log.Info("webhook registration suppressed", "webhook", e.name)
			continue
		}
		if err := e.register(mgr); err != nil {
			return err
		}
	}

	return nil
}
