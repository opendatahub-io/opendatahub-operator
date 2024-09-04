package capabilities

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-multierror"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Registry used by ODH Components to register their capabilities configuration.
type Registry struct {
	orchestrator  *PlatformOrchestrator
	authorization *AuthorizationCapability
	routing       *RoutingCapability
	owner         metav1.Object
	appNs         string
}

// NewRegistry creates a new Registry instance with the provided capabilities as required parameters, and allows to pass
// optional configuration through RegistryOption functions.
func NewRegistry(authz *AuthorizationCapability, routing *RoutingCapability, owner metav1.Object, orchestrator *PlatformOrchestrator, options ...RegistryOption) *Registry {
	registry := Registry{
		authorization: authz,
		routing:       routing,
		owner:         owner,
		orchestrator:  orchestrator,
		appNs:         "opendatahub",
	}

	for _, option := range options {
		option(&registry)
	}

	return &registry
}

// RegistryOption is a function that allows to enrich the logic of constructing the Registry struct.
type RegistryOption func(*Registry)

func WithAppNamespace(appNs string) RegistryOption {
	return func(r *Registry) {
		r.appNs = appNs
	}
}

// Configure enables the platform capabilities for registered components.
func (r *Registry) Configure(ctx context.Context, cli client.Client) error {
	handlers := []Reconciler{r.authorization, r.routing}

	var errReconcile *multierror.Error
	for _, handler := range handlers {
		errReconcile = multierror.Append(errReconcile, handler.Reconcile(ctx, cli, r.owner))
	}

	if errStart := r.configureCtrls(ctx, cli); errStart != nil {
		errReconcile = multierror.Append(errReconcile, errStart)
	}

	if errReconcile.ErrorOrNil() != nil {
		return fmt.Errorf("failed enabling platform capabilities: %w", errReconcile)
	}

	return nil
}

func (r *Registry) configureCtrls(ctx context.Context, cli client.Client) error {
	if errAuthzStart := r.orchestrator.ToggleAuthorization(ctx, cli, r.authorization.config, r.authorization.protectedResources...); errAuthzStart != nil {
		return fmt.Errorf("failed to start authorization controllers: %w", errAuthzStart)
	}

	if errRoutingStart := r.orchestrator.ToggleRouting(ctx, cli, r.routing.IngressConfig(), r.routing.RoutingTargets()...); errRoutingStart != nil {
		return fmt.Errorf("failed to start authorization controllers: %w", errRoutingStart)
	}

	return nil
}

// Component view of the platform capabilities.

var _ PlatformCapabilities = (*Registry)(nil)

// Authorization returns the "component view" of this capability.
func (r *Registry) Authorization() Authorization {
	return r.authorization
}

// Routing returns the "component view" of this capability.
func (r *Registry) Routing() Routing {
	return r.routing
}
