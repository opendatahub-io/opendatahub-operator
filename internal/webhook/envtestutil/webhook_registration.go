package envtestutil

import (
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	hardwareprofilewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/hardwareprofile"
	monitoringwebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/monitoring"
)

// RegisterWebhooks registers platform webhook handlers for envtest integration tests.
//
// This function is specifically designed for tests that create Kubernetes resources
// (such as InferenceServices) that are targeted by multiple webhook configurations.
// In a real cluster, when these resources are created, Kubernetes will attempt to call
// all relevant webhooks. To properly simulate this behavior in envtest, all webhook handlers
// must be registered, even if the test is primarily focused on one webhook's functionality.
//
// Use this function when:
//   - Testing hardware profile injection functionality
//   - Testing InferenceService or LLMInferenceService creation with hardware profiles
//   - Testing any workflow that creates resources matching multiple webhook selectors
//   - You need all webhooks to be available to avoid "webhook endpoint not found" errors
func RegisterWebhooks(mgr manager.Manager) error {
	hardwareProfileInjector := &hardwareprofilewebhook.Injector{
		Client:  mgr.GetAPIReader(),
		Decoder: admission.NewDecoder(mgr.GetScheme()),
		Name:    "hardwareprofile-injector",
	}
	if err := hardwareProfileInjector.SetupWithManager(mgr); err != nil {
		return err
	}

	monitoringInjector := &monitoringwebhook.Injector{
		Client:  mgr.GetAPIReader(),
		Decoder: admission.NewDecoder(mgr.GetScheme()),
		Name:    "monitoring-injector",
	}
	if err := monitoringInjector.SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
