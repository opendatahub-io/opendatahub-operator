package capabilities

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-multierror"
	"github.com/opendatahub-io/odh-platform/pkg/routing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Registry used by Components to register their Capabilities configuration.
type Registry struct {
	owner         metav1.Object
	appNs         string
	authorization *AuthorizationCapability
	routing       *RoutingCapability
	// Tmp solution to allow the registry to interact with the orchestrator when ctrls are embedded as library
	orchestrator *PlatformOrchestrator
}

func NewRegistry(authz *AuthorizationCapability, routing *RoutingCapability, options ...RegistryOption) *Registry {
	registry := Registry{
		authorization: authz,
		routing:       routing,
	}

	for _, option := range options {
		option(&registry)
	}

	return &registry
}

type RegistryOption func(*Registry)

func WithOwner(owner metav1.Object) RegistryOption {
	return func(r *Registry) {
		r.owner = owner
	}
}

func WithAppNamespace(appNs string) RegistryOption {
	return func(r *Registry) {
		r.appNs = appNs
	}
}

func WithOrchestrator(orchestrator *PlatformOrchestrator) RegistryOption {
	return func(r *Registry) {
		r.orchestrator = orchestrator
	}
}

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

	// TODO(mvp): move up as part of r.routing - do not pass spec
	config := routing.PlatformRoutingConfiguration{
		IngressSelectorLabel: r.routing.routingSpec.IngressGateway.LabelSelectorKey,
		IngressSelectorValue: r.routing.routingSpec.IngressGateway.LabelSelectorValue,
		IngressService:       r.routing.routingSpec.IngressGateway.Name,
		GatewayNamespace:     r.routing.routingSpec.IngressGateway.Namespace,
	}

	if errRoutingStart := r.orchestrator.ToggleRouting(ctx, cli, config, r.routing.routingTargets...); errRoutingStart != nil {
		return fmt.Errorf("failed to start authorization controllers: %w", errRoutingStart)
	}

	return nil
}

var _ PlatformCapabilities = (*Registry)(nil)

// Authorization returns the "component view" of the capability.
func (r *Registry) Authorization() Authorization {
	return r.authorization
}

// Routing returns the "component view" of the capability.
func (r *Registry) Routing() Routing {
	return r.routing
}
