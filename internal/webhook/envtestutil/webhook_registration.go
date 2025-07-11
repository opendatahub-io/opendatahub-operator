package envtestutil

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	hardwareprofilewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/hardwareprofile"
	kueuewebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/kueue"
)

// WebhookHandler represents a webhook handler that can be set up with a manager.
// This interface is satisfied by all webhook handlers in the codebase.
type WebhookHandler interface {
	SetupWithManager(mgr manager.Manager) error
}

// DecoderInjectableHandler represents a webhook handler that needs decoder injection.
// This interface is satisfied by handlers that implement the InjectDecoder method.
// Only webhooks that actually decode request objects need this (e.g., auth, hardware profile, dscinitialization).
// Webhooks that use direct request decoding (e.g., kueue) don't need decoder injection.
type DecoderInjectableHandler interface {
	WebhookHandler
	InjectDecoder(decoder admission.Decoder) error
}

// RegistrationOption is a functional option for webhook registration.
type RegistrationOption func(*registrationConfig)

// registrationConfig holds the configuration for webhook registration.
type registrationConfig struct {
	handlers []WebhookHandler
}

// WithHandlers adds webhook handlers to be registered.
func WithHandlers(handlers ...WebhookHandler) RegistrationOption {
	return func(config *registrationConfig) {
		config.handlers = append(config.handlers, handlers...)
	}
}

// RegisterWebhooksWithManualDecoder registers webhooks in envtest environments using options.
// It automatically detects which handlers need decoder injection.
//
// Parameters:
//   - mgr: The controller-runtime manager to register webhooks with.
//   - opts: Options specifying which handlers to register.
//
// Returns:
//   - error: Any error encountered during webhook registration or decoder injection.
func RegisterWebhooksWithManualDecoder(mgr manager.Manager, opts ...RegistrationOption) error {
	// Apply options
	config := &registrationConfig{}
	for _, opt := range opts {
		opt(config)
	}

	var decoderInjectableHandlers []DecoderInjectableHandler

	// Auto-detect which handlers need decoder injection
	for _, handler := range config.handlers {
		if decoderHandler, ok := handler.(DecoderInjectableHandler); ok {
			decoderInjectableHandlers = append(decoderInjectableHandlers, decoderHandler)
		}
	}

	// Create decoder only if needed
	var decoder admission.Decoder
	if len(decoderInjectableHandlers) > 0 {
		decoder = admission.NewDecoder(mgr.GetScheme())
	}

	// Register all handlers
	for _, handler := range config.handlers {
		// If handler needs decoder injection, inject it first
		if decoderHandler, ok := handler.(DecoderInjectableHandler); ok {
			if err := decoderHandler.InjectDecoder(decoder); err != nil {
				return fmt.Errorf("failed to inject decoder for handler: %w", err)
			}
		}

		// Register the handler with the manager
		if err := handler.SetupWithManager(mgr); err != nil {
			return fmt.Errorf("failed to setup handler with manager: %w", err)
		}
	}

	return nil
}

// RegisterKueueWebhook registers only the Kueue webhook for integration testing.
//
// This function is designed for tests that only need Kueue validation functionality and don't create
// resources that would trigger the hardware profile webhook. It's more lightweight than registering
// both webhooks when only Kueue functionality is being tested.
//
// Use this function when:
//   - Testing pure Kueue validation logic
//   - You only need to validate Kueue labels on resources
//   - You want to isolate Kueue webhook behavior from other webhooks
func RegisterKueueWebhook(mgr manager.Manager) error {
	return RegisterWebhooksWithManualDecoder(mgr,
		WithHandlers(
			&kueuewebhook.Validator{
				Client: mgr.GetAPIReader(),
				Name:   "kueue-validating",
			},
		),
	)
}

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
//
// The function automatically handles decoder injection for webhooks that require it.
func RegisterHardwareProfileAndKueueWebhooks(mgr manager.Manager) error {
	return RegisterWebhooksWithManualDecoder(mgr,
		WithHandlers(
			&kueuewebhook.Validator{
				Client: mgr.GetAPIReader(),
				Name:   "kueue-validating",
			},
			&hardwareprofilewebhook.Injector{
				Client: mgr.GetAPIReader(),
				Name:   "hardwareprofile-injector",
			},
		),
	)
}
