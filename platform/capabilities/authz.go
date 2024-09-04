package capabilities

import (
	"context"
	"fmt"

	"github.com/opendatahub-io/odh-platform/pkg/authorization"
	"github.com/opendatahub-io/odh-platform/pkg/platform"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

// Authorization is component-facing interface allowing ODH components to enroll to platform's authorization capability.
type Authorization interface {
	IsAvailable() bool
	ProtectedResources(protectedResource ...platform.ProtectedResource)
}

func NewAuthorization(config authorization.ProviderConfig, available bool) *AuthorizationCapability {
	return &AuthorizationCapability{
		available: available,
		config:    config,
	}
}

type AuthorizationCapability struct {
	available          bool
	config             authorization.ProviderConfig
	protectedResources []platform.ProtectedResource
}

// Component registration API.
var _ Authorization = (*AuthorizationCapability)(nil)

func (a *AuthorizationCapability) IsAvailable() bool {
	return a.available
}

func (a *AuthorizationCapability) ProtectedResources(protectedResource ...platform.ProtectedResource) {
	a.protectedResources = append(a.protectedResources, protectedResource...)
}

// Platform configuration managed by the operator.
var _ Reconciler = (*AuthorizationCapability)(nil)

func (a *AuthorizationCapability) IsRequired() bool {
	return len(a.protectedResources) > 0
}

// Reconcile ensures Authorization capability and component-specific configuration is wired when needed.
func (a *AuthorizationCapability) Reconcile(ctx context.Context, cli client.Client, owner metav1.Object) error {
	const roleName = "platform-protected-resources-watcher"

	withOwnerRef, err := cluster.AsOwnerRef(owner)
	if err != nil {
		return fmt.Errorf("failed to define meta options while reconciling authorization capability: %w", err)
	}

	objectReferences := make([]platform.ResourceReference, len(a.protectedResources))
	for i, ref := range a.protectedResources {
		objectReferences[i] = ref.ResourceReference
	}

	// TODO: check if it is safe to delete roles. We have running (but potentially deactivated) controllers for the given resources,
	// so we keep the roles after first creation.
	return CreateOrUpdatePlatformRBAC(ctx, cli, roleName, objectReferences, withOwnerRef)
}
