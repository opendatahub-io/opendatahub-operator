package envtestutil

import (
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	hardwareprofilewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/hardwareprofile"
	inferenceservicewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/inferenceservice"
	kueuewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/kueue"
)

// RegisterHardwareProfileAndKueueWebhooks registers hardware profile, Kueue, and connection webhooks for integration testing.
//
// This function is specifically designed for tests that create Kubernetes resources (such as Notebooks or InferenceServices)
// that are targeted by multiple webhook configurations. In a real cluster, when these resources are created, Kubernetes
// will attempt to call all relevant webhooks. To properly simulate this behavior in envtest, all webhook handlers must be
// registered, even if the test is primarily focused on one webhook's functionality.
//
// Use this function when:
//   - Testing hardware profile injection functionality (which creates Notebooks)
//   - Testing InferenceService creation with hardware profiles
//   - Testing any workflow that creates resources matching multiple webhook selectors
//   - You need all webhooks to be available to avoid "webhook endpoint not found" errors
func RegisterHardwareProfileAndKueueWebhooks(mgr manager.Manager) error {
	// Register Kueue webhook
	kueueValidator := &kueuewebhook.Validator{
		Client:  mgr.GetAPIReader(),
		Decoder: admission.NewDecoder(mgr.GetScheme()),
		Name:    "kueue-validating",
	}
	if err := kueueValidator.SetupWithManager(mgr); err != nil {
		return err
	}

	// Register Hardware Profile webhook
	hardwareProfileInjector := &hardwareprofilewebhook.Injector{
		Client:  mgr.GetAPIReader(),
		Decoder: admission.NewDecoder(mgr.GetScheme()),
		Name:    "hardwareprofile-injector",
	}
	if err := hardwareProfileInjector.SetupWithManager(mgr); err != nil {
		return err
	}

	// Register Connection webhook for InferenceService
	connectionWebhook := &inferenceservicewebhook.ConnectionWebhook{
		Client:  mgr.GetAPIReader(),
		Decoder: admission.NewDecoder(mgr.GetScheme()),
		Name:    "connection-isvc",
	}
	if err := connectionWebhook.SetupWithManager(mgr); err != nil {
		return err
	}

	return nil
}
