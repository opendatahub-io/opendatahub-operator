package dscinitialization

import (
	"context"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

var (
	// adminGroups maps each supported platform to its default admin group name.
	// These groups are assigned administrative privileges in the Auth custom resource.
	//
	// Platform mappings:
	//   - SelfManagedRhoai: "rhods-admins" - for self-managed Red Hat OpenShift AI
	//   - ManagedRhoai: "dedicated-admins" - for managed/hosted Red Hat OpenShift AI
	//   - OpenDataHub: "odh-admins" - for Open Data Hub deployments
	//
	// The admin group members have full administrative access to the platform,
	// including the ability to manage users, configure components, and access
	// all platform resources.
	adminGroups = map[common.Platform]string{
		cluster.SelfManagedRhoai: "rhods-admins",
		cluster.ManagedRhoai:     "dedicated-admins",
		cluster.OpenDataHub:      "odh-admins",
	}
)

// ManageAuthCR manages the Auth custom resource based on authentication method.
// For IntegratedOAuth: creates Auth CR with platform-specific admin groups if it doesn't exist.
// For external OIDC: deletes Auth CR if it exists (cleanup).
//
// Parameters:
//   - ctx: Context for the operation
//   - platform: The target platform type used to determine admin group configuration
//   - isIntegratedOAuth: true if using IntegratedOAuth, false for external OIDC
//
// Returns:
//   - error: nil on success, error if creation or deletion fails
func (r *DSCInitializationReconciler) ManageAuthCR(ctx context.Context, platform common.Platform, isIntegratedOAuth bool) error {
	authCR := &serviceApi.Auth{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.AuthInstanceName,
		},
	}

	err := r.Client.Get(ctx, client.ObjectKey{Name: serviceApi.AuthInstanceName}, authCR)
	if err != nil && !k8serr.IsNotFound(err) {
		return err
	}

	// Auth CR does not exist
	if k8serr.IsNotFound(err) {
		if isIntegratedOAuth { // create Auth CR for IntegratedOAuth or do nothing.
			if err := r.Client.Create(ctx, BuildDefaultAuth(platform)); err != nil && !k8serr.IsAlreadyExists(err) {
				return err
			}
		}
		return nil
	}

	// Auth CR exists
	if isIntegratedOAuth { // do nothing or delete Auth CR for OIDC.
		return nil
	}
	if err := r.Client.Delete(ctx, authCR); err != nil && !k8serr.IsNotFound(err) {
		return err
	}
	return nil
}

// BuildDefaultAuth creates a default Auth custom resource with platform-specific configuration.
//
// Parameters:
//   - platform: The target platform type (OpenDataHub, SelfManagedRhoai, or ManagedRhoai)
//
// Returns:
//   - client.Object: A serviceApi.Auth resource with platform-specific admin group and system:authenticated in allowed groups
func BuildDefaultAuth(platform common.Platform) client.Object {
	// Get admin group for the platform, with fallback to OpenDataHub admin group
	adminGroup := adminGroups[platform]
	if adminGroup == "" {
		adminGroup = adminGroups[cluster.OpenDataHub]
	}

	return &serviceApi.Auth{
		TypeMeta:   metav1.TypeMeta{Kind: serviceApi.AuthKind, APIVersion: serviceApi.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.AuthInstanceName},
		Spec: serviceApi.AuthSpec{
			AdminGroups:   []string{adminGroup},
			AllowedGroups: []string{"system:authenticated"},
		},
	}
}
