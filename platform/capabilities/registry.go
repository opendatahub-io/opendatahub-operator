package capabilities

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/go-multierror"
	"github.com/opendatahub-io/odh-platform/pkg/spi"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
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

func Owner(owner metav1.Object) RegistryOption {
	return func(r *Registry) {
		r.owner = owner
	}
}

func AppNamespace(appNs string) RegistryOption {
	return func(r *Registry) {
		r.appNs = appNs
	}
}

func Orchestrator(orchestrator *PlatformOrchestrator) RegistryOption {
	return func(r *Registry) {
		r.orchestrator = orchestrator
	}
}

func (r *Registry) ConfigureCapabilities(ctx context.Context, cli client.Client) error {
	metaOptions, err := createMetaOptions(r)
	if err != nil {
		return err
	}

	handlers := []Reconciler{r.authorization, r.routing}

	var errReconcile *multierror.Error
	for _, handler := range handlers {
		errReconcile = multierror.Append(errReconcile, handler.Reconcile(ctx, cli, r.owner))
	}

	if errStart := r.startControllers(ctx, cli, metaOptions...); errStart != nil {
		errReconcile = multierror.Append(errReconcile, errStart)
	}

	if errReconcile.ErrorOrNil() != nil {
		return fmt.Errorf("failed enabling platform capabilities: %w", errReconcile)
	}

	return nil
}

func (r *Registry) startControllers(ctx context.Context, cli client.Client, metaOptions ...cluster.MetaOptions) error {
	if r.authorization.IsRequired() {
		if errAuthzStart := r.orchestrator.StartAuthorization(ctx, cli, r.authorization.config, r.authorization.protectedResources...); errAuthzStart != nil {
			return fmt.Errorf("failed to start authorization controllers: %w", errAuthzStart)
		}
	}

	if r.routing.IsRequired() {
		// TODO(mvp): move up as part of r.routing - do not pass spec
		config := spi.PlatformRoutingConfiguration{
			IngressSelectorLabel: r.routing.routingSpec.IngressGateway.LabelSelectorKey,
			IngressSelectorValue: r.routing.routingSpec.IngressGateway.LabelSelectorValue,
			IngressService:       r.routing.routingSpec.IngressGateway.Name,
			GatewayNamespace:     r.routing.routingSpec.IngressGateway.Namespace,
		}
		if errRoutingStart := r.orchestrator.StartRouting(ctx, cli, config, r.routing.routingTargets...); errRoutingStart != nil {
			return fmt.Errorf("failed to start authorization controllers: %w", errRoutingStart)
		}
	}

	// TODO(mvp): should we leave it?
	platformSettings := make(map[string]string)

	authzJSON, errJSONAuthz := json.Marshal(r.authorization.protectedResources)
	if errJSONAuthz != nil {
		return errJSONAuthz
	}
	platformSettings["authorization"] = string(authzJSON)

	routingJSON, errJSONRouting := json.Marshal(r.routing.routingTargets)
	if errJSONRouting != nil {
		return errJSONRouting
	}
	platformSettings["routing"] = string(routingJSON)

	return cluster.CreateOrUpdateConfigMap(
		ctx,
		cli,
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "platform-capabilities",
				Namespace: r.appNs, // TODO isn't it the other ns?
			},
			Data: platformSettings,
		},
		metaOptions...,
	)
}

// TODO: re-work.
func createMetaOptions(r *Registry) ([]cluster.MetaOptions, error) {
	var metaOptions []cluster.MetaOptions

	if r.owner != nil {
		ownerRef, err := cluster.ToOwnerReference(r.owner)
		if err != nil {
			return nil, err
		}
		metaOptions = append(metaOptions, cluster.WithOwnerReference(ownerRef))
	}

	return metaOptions, nil
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
