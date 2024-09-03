package capabilities

import (
	"context"
	"fmt"

	"github.com/opendatahub-io/odh-platform/pkg/authorization"
	"github.com/opendatahub-io/odh-platform/pkg/platform"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NewAuthorization(available bool, opts ...AuthzOption) *AuthorizationCapability {
	authzCapability := &AuthorizationCapability{available: available}
	for _, opt := range opts {
		opt(authzCapability)
	}

	return authzCapability
}

type Authorization interface {
	IsAvailable() bool
	ProtectedResources(protectedResource ...platform.ProtectedResource)
}

type AuthzOption func(*AuthorizationCapability)

type AuthorizationCapability struct {
	available          bool
	config             authorization.ProviderConfig
	protectedResources []platform.ProtectedResource
}

func WithAuthzConfig(config authorization.ProviderConfig) AuthzOption {
	return func(a *AuthorizationCapability) {
		a.config = config
	}
}

// Component registration API.
var _ Authorization = (*AuthorizationCapability)(nil)

func (a *AuthorizationCapability) IsAvailable() bool {
	return a.available
}

func (a *AuthorizationCapability) ProtectedResources(protectedResource ...platform.ProtectedResource) {
	a.protectedResources = append(a.protectedResources, protectedResource...)
}

// Platform configuration by the operator.
var _ Reconciler = (*AuthorizationCapability)(nil)

func (a *AuthorizationCapability) IsRequired() bool {
	return len(a.protectedResources) > 0
}

// Reconcile ensures Authorization capability and component-specific configuration is wired when needed.
func (a *AuthorizationCapability) Reconcile(ctx context.Context, cli client.Client, owner metav1.Object) error {
	const roleName = "platform-protected-resources-watcher"

	metaOpts, err := defineMetaOptions(owner)
	if err != nil {
		return fmt.Errorf("failed to define meta options while reconciling authorization capability: %w", err)
	}

	objectReferences := make([]platform.ResourceReference, len(a.protectedResources))
	for i, ref := range a.protectedResources {
		objectReferences[i] = ref.ResourceReference
	}

	// TODO: check if it is safe to delete roles. We have running (but potentially deactivated) controllers for the given resources,
	// so we keep the roles after first creation.
	return CreateOrUpdatePlatformRBAC(ctx, cli, roleName, objectReferences, metaOpts...)
}
