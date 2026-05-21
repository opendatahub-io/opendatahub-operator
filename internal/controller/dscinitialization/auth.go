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

	// allowedGroups maps each supported platform to its default allowed group(s).
	// These groups are assigned general user access privileges in the Auth custom resource.
	//
	// SECURITY NOTE: Replaces the deprecated 'system:authenticated' group which violated
	// Kubernetes security best practices by granting access to any authenticated user.
	//
	// Platform mappings:
	//   - SelfManagedRhoai: "rhods-users" - for Red Hat OpenShift AI users
	//   - ManagedRhoai: "rhods-users" - for Red Hat OpenShift AI users
	//   - OpenDataHub: "odh-users" - for Open Data Hub users
	allowedGroups = map[common.Platform][]string{
		cluster.SelfManagedRhoai: {"rhods-users"},
		cluster.ManagedRhoai:     {"rhods-users"},
		cluster.OpenDataHub:      {"odh-users"},
	}
)

// CreateAuth ensures an Auth custom resource exists in the cluster.
//
// Parameters:
//   - ctx: Context for the operation
//   - platform: The target platform type used to determine admin group configuration
//
// Returns:
//   - error: nil on success, error if Auth CR creation fails
func (r *DSCInitializationReconciler) CreateAuth(ctx context.Context, platform common.Platform) error {
	a := serviceApi.Auth{}
	// Auth CR exists, we do nothing
	err := r.Client.Get(ctx, client.ObjectKey{Name: serviceApi.AuthInstanceName}, &a)
	if err == nil {
		return nil
	}

	if !k8serr.IsNotFound(err) {
		return err
	}
	// Auth CR not found, create default Auth CR
	if err := r.Client.Create(ctx, BuildDefaultAuth(platform)); err != nil && !k8serr.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// BuildDefaultAuth creates a default Auth custom resource with platform-specific configuration.
//
// SECURITY UPDATE: Now uses explicit platform-specific groups instead of 'system:authenticated'
// to follow Kubernetes security best practices.
//
// Parameters:
//   - platform: The target platform type (OpenDataHub, SelfManagedRhoai, or ManagedRhoai)
//
// Returns:
//   - client.Object: A serviceApi.Auth resource with platform-specific admin and allowed groups
func BuildDefaultAuth(platform common.Platform) client.Object {
	// Get admin group for the platform, with fallback to OpenDataHub admin group
	adminGroup := adminGroups[platform]
	if adminGroup == "" {
		adminGroup = adminGroups[cluster.OpenDataHub]
	}

	// Get allowed groups for the platform, with fallback to OpenDataHub allowed groups
	allowedGroupsList := allowedGroups[platform]
	if len(allowedGroupsList) == 0 {
		allowedGroupsList = allowedGroups[cluster.OpenDataHub]
	}

	return &serviceApi.Auth{
		TypeMeta: metav1.TypeMeta{Kind: serviceApi.AuthKind, APIVersion: serviceApi.GroupVersion.String()},
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.AuthInstanceName,
			Annotations: map[string]string{
				"auth.opendatahub.io/migration-note": "Default groups changed from 'system:authenticated' to platform-specific groups for security compliance",
			},
		},
		Spec: serviceApi.AuthSpec{
			AdminGroups:   []string{adminGroup},
			AllowedGroups: allowedGroupsList,
		},
	}
}
