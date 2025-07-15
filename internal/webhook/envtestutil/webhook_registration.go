package envtestutil

import (
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	kueuewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/kueue"
)

// RegisterHardwareProfileAndKueueWebhooks registers both hardware profile and Kueue webhooks for integration testing.
//
// This function is specifically designed for tests that create Kubernetes resources (such as Notebooks or InferenceServices)
// that are targeted by both webhook configurations. In a real cluster, when these resources are created, Kubernetes
// will attempt to call both webhooks. To properly simulate this behavior in envtest, both webhook handlers must be
// registered, even if the test is primarily focused on one webhook's functionality.
//
// Use this function when:
//   - Testing hardware profile injection functionality (which creates Notebooks)
//   - Testing any workflow that creates resources matching both webhook selectors
//   - You need both webhooks to be available to avoid "webhook endpoint not found" errors
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

	return nil
}
