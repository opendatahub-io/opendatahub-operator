package capabilities

import (
	"context"
	"fmt"

	"github.com/opendatahub-io/odh-platform/controllers/authorization"
	"github.com/opendatahub-io/odh-platform/pkg/platform"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

func NewAuthorization(available bool, opts ...AuthzOption) *AuthorizationCapability {
	authzCapability := &AuthorizationCapability{available: available}
	for _, opt := range opts {
		opt(authzCapability)
	}

	return authzCapability
}

type Authorization interface {
	Availability
	ProtectedResources(protectedResource ...platform.ProtectedResource)
}

type AuthzOption func(*AuthorizationCapability)

// Producer

var _ Authorization = (*AuthorizationCapability)(nil)

type AuthorizationCapability struct {
	available          bool
	config             authorization.PlatformAuthorizationConfig
	protectedResources []platform.ProtectedResource
}

func (a *AuthorizationCapability) IsAvailable() bool {
	return a.available
}

func (a *AuthorizationCapability) ProtectedResources(protectedResource ...platform.ProtectedResource) {
	a.protectedResources = append(a.protectedResources, protectedResource...)
}

func WithAuthzConfig(config authorization.PlatformAuthorizationConfig) AuthzOption {
	return func(a *AuthorizationCapability) {
		a.config = config
	}
}

// Consumer

var _ Reconciler = (*AuthorizationCapability)(nil)

func (a *AuthorizationCapability) IsRequired() bool {
	return len(a.protectedResources) > 0
}

// Reconcile ensures Authorization capability and component-specific configuration is wired when needed.
func (a *AuthorizationCapability) Reconcile(ctx context.Context, cli client.Client, owner metav1.Object) error {
	const roleName = "platform-protected-resources-watcher"

	if a.IsRequired() {
		ownerRef, err := cluster.ToOwnerReference(owner)
		if err != nil {
			return fmt.Errorf("failed to create owner reference while reconciling Authorization capability: %w", err)
		}

		objectReferences := make([]platform.ObjectReference, len(a.protectedResources))
		for i, ref := range a.protectedResources {
			objectReferences[i] = ref.ObjectReference
		}

		return CreateOrUpdatePlatformRoleBindings(ctx, cli, roleName, objectReferences, cluster.WithOwnerReference(ownerRef))
	}

	return DeletePlatformRoleBindings(ctx, cli, roleName)
}
