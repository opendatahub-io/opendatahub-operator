package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	// Default admin group names for different platforms.
	rhodsAdminsName     = "rhods-admins"
	odhAdminsName       = "odh-admins"
	dedicatedAdminsName = "dedicated-admins"

	// Default allowed group names for different platforms.
	rhodsUsersGroupName = "rhods-users"
	odhUsersGroupName   = "odh-users"

	// Deprecated group name (no longer used in defaults, filtered by controller).
	systemAuthenticatedGroup = "system:authenticated"

	// Role names used for RBAC configuration.
	adminGroupRoleName = "data-science-admingroup-role"

	// RoleBinding names to bind roles to specific groups.
	adminGroupRoleBindingName = "data-science-admingroup-rolebinding"

	// ClusterRole and ClusterRoleBinding names for group access at cluster level.
	adminGroupClusterRoleName          = "data-science-admingroupcluster-role"
	adminGroupClusterRoleBindingName   = "data-science-admingroupcluster-rolebinding"
	allowedGroupClusterRoleName        = "data-science-allowedgroupcluster-role"
	allowedGroupClusterRoleBindingName = "data-science-allowedgroupcluster-rolebinding"
)

type AuthControllerTestCtx struct {
	*TestContext

	AuthNamespacedName types.NamespacedName
}

func authControllerTestSuite(t *testing.T) {
	t.Helper()

	// Initialize the test context.
	tc, err := NewTestContext(t)
	require.NoError(t, err, "Failed to initialize test context")

	// Create an instance of test context.
	authCtx := AuthControllerTestCtx{
		TestContext: tc,
		// Auth is cluster-scoped, no namespace needed
		AuthNamespacedName: types.NamespacedName{Name: serviceApi.AuthInstanceName},
	}

	// Define test cases.
	testCases := []TestCase{
		{"Validate Auth system initialization", authCtx.ValidateAuthSystemInitialization},
		{"Validate addition of RoleBinding when group is added", authCtx.ValidateAddingGroups},
		{"Validate removal of bindings when a group is removed", authCtx.ValidateRemovingGroups},
		{"Validate CEL blocks Auth update with invalid groups", authCtx.ValidateCELBlocksInvalidGroupsViaUpdate},
		{"Validate CEL allows Auth with valid groups", authCtx.ValidateCELAllowsValidGroups},
		{"Validate controller security filtering", authCtx.ValidateControllerSecurityFiltering},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateAuthSystemInitialization ensures RBAC resources are correctly configured.
func (tc *AuthControllerTestCtx) ValidateAuthSystemInitialization(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke)

	expectedAdminGroup := tc.getExpectedAdminGroupForPlatform()
	expectedAllowedGroup := tc.getExpectedAllowedGroupForPlatform()

	// 1. Validate that exactly one Auth CR exists with correct default content
	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Auth, types.NamespacedName{}),
		WithCondition(And(
			HaveLen(1), // Ensure exactly one Auth CR exists
			// Validate the content of that single Auth CR
			HaveEach(And(
				jq.Match(`.spec.adminGroups | index("%s") != null`, expectedAdminGroup),
				jq.Match(`.spec.allowedGroups | index("%s") != null`, expectedAllowedGroup),
			)),
		)),
		WithCustomErrorMsg(
			"Expected exactly one Auth CR with adminGroups=['%s'] and allowedGroups=['%s'], but validation failed",
			expectedAdminGroup,
			expectedAllowedGroup,
		),
	)

	// 2. Validate RBAC infrastructure - Roles are created
	roles := []string{adminGroupRoleName}
	for _, roleName := range roles {
		tc.validateRBACResource(gvk.Role, roleName)
	}

	// 3. Validate RBAC infrastructure - RoleBindings are created
	roleBindings := []string{adminGroupRoleBindingName}
	for _, roleBinding := range roleBindings {
		tc.validateRBACResource(gvk.RoleBinding, roleBinding)
	}

	// 4. Validate cluster-level RBAC infrastructure - ClusterRole is created
	tc.validateRBACResource(gvk.ClusterRole, adminGroupClusterRoleName)
	tc.validateRBACResource(gvk.ClusterRole, allowedGroupClusterRoleName)

	// 5. Validate cluster-level RBAC infrastructure - ClusterRoleBinding is created
	tc.validateRBACResource(gvk.ClusterRoleBinding, adminGroupClusterRoleBindingName)
	tc.validateRBACResource(gvk.ClusterRoleBinding, allowedGroupClusterRoleBindingName)
}

// ValidateAddingGroups adds groups and validates.
func (tc *AuthControllerTestCtx) ValidateAddingGroups(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	testAdminGroup := "aTestAdminGroup"
	testAllowedGroup := "aTestAllowedGroup"

	// Update the Auth CR with new admin and allowed groups.
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.Auth, tc.AuthNamespacedName),
		WithMutateFunc(
			testf.Transform(
				`.spec.adminGroups |= . + ["%s"] | .spec.allowedGroups |= . + ["%s"]`, testAdminGroup, testAllowedGroup,
			),
		),
	)

	// Validate the RoleBinding and ClusterRoleBinding for admin and allowed groups.
	tc.validateRBACResource(gvk.RoleBinding, adminGroupRoleBindingName,
		WithCondition(jq.Match(`.subjects | map(.name) | index("%s") != null`, testAdminGroup)))
	tc.validateRBACResource(gvk.ClusterRoleBinding, adminGroupClusterRoleBindingName,
		WithCondition(jq.Match(`.subjects | map(.name) | index("%s") != null`, testAdminGroup)))
	tc.validateRBACResource(gvk.ClusterRoleBinding, allowedGroupClusterRoleBindingName,
		WithCondition(jq.Match(`.subjects | map(.name) | index("%s") != null`, testAllowedGroup)))
}

// ValidateRemovingGroups removes groups from Auth CR and validates the changes.
func (tc *AuthControllerTestCtx) ValidateRemovingGroups(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	// Get the expected admin group for the current platform
	expectedGroup := tc.getExpectedAdminGroupForPlatform()

	// Update the Auth CR to set only the expected admin group.
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.Auth, tc.AuthNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.adminGroups = ["%s"]`, expectedGroup)),
		WithCustomErrorMsg("Failed to create or update Auth resource '%s' with admin group '%s'", serviceApi.AuthInstanceName, expectedGroup),
	)

	// Validate RoleBinding and ClusterRoleBinding for admin group after removal.
	singleSubjectCondition := WithCondition(And(
		jq.Match(`.subjects | length == 1`),
		jq.Match(`.subjects | map(select(.name == "%s")) | length == 1`, expectedGroup),
	))

	tc.validateRBACResource(gvk.RoleBinding, adminGroupRoleBindingName,
		singleSubjectCondition,
		WithCustomErrorMsg("Expected %s '%s' to have exactly one subject with name '%s'", gvk.RoleBinding.Kind, adminGroupRoleBindingName, expectedGroup))
	tc.validateRBACResource(gvk.ClusterRoleBinding, adminGroupClusterRoleBindingName,
		singleSubjectCondition,
		WithCustomErrorMsg("Expected %s '%s' to have exactly one subject with name '%s'", gvk.ClusterRoleBinding.Kind, adminGroupClusterRoleBindingName, expectedGroup))
}

// ValidateControllerSecurityFiltering validates that the controller properly filters
// system:authenticated from role bindings even if it were present in the Auth CR.
// This tests the defense-in-depth security filtering implemented in the controller.
func (tc *AuthControllerTestCtx) ValidateControllerSecurityFiltering(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	expectedAdminGroup := tc.getExpectedAdminGroupForPlatform()
	expectedAllowedGroup := tc.getExpectedAllowedGroupForPlatform()

	// Reset to defaults first to ensure clean state
	tc.resetAuthToDefaults(t)

	// Verify that the controller has properly filtered system:authenticated and that
	// only platform-appropriate groups are bound to cluster roles.

	// Validate AdminGroup ClusterRoleBinding subjects
	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.ClusterRoleBinding, types.NamespacedName{Name: adminGroupClusterRoleBindingName}),
		WithCondition(And(
			// Ensure no system:authenticated is present in admin role binding subjects
			jq.Match(`.subjects | map(select(.name == "%s")) | length == 0`, systemAuthenticatedGroup),
			// Ensure the expected admin group is present
			jq.Match(`.subjects | map(select(.name == "%s")) | length == 1`, expectedAdminGroup),
		)),
		WithCustomErrorMsg(
			"Admin ClusterRoleBinding should not contain system:authenticated and should contain expected admin group '%s'",
			expectedAdminGroup,
		),
	)

	// Validate AllowedGroup ClusterRoleBinding subjects
	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.ClusterRoleBinding, types.NamespacedName{Name: allowedGroupClusterRoleBindingName}),
		WithCondition(And(
			// Ensure no system:authenticated is present in allowed group role binding subjects
			jq.Match(`.subjects | map(select(.name == "%s")) | length == 0`, systemAuthenticatedGroup),
			// Ensure the expected allowed group is present
			jq.Match(`.subjects | map(select(.name == "%s")) | length == 1`, expectedAllowedGroup),
		)),
		WithCustomErrorMsg(
			"Allowed ClusterRoleBinding should not contain system:authenticated and should contain expected allowed group '%s'",
			expectedAllowedGroup,
		),
	)

	// Validate namespace-scoped RoleBinding subjects
	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.RoleBinding, types.NamespacedName{Name: adminGroupRoleBindingName, Namespace: tc.AppsNamespace}),
		WithCondition(And(
			// Ensure no system:authenticated is present in role binding subjects
			jq.Match(`.subjects | map(select(.name == "%s")) | length == 0`, systemAuthenticatedGroup),
			// Ensure the expected admin group is present
			jq.Match(`.subjects | map(select(.name == "%s")) | length == 1`, expectedAdminGroup),
		)),
		WithCustomErrorMsg(
			"Admin RoleBinding should not contain system:authenticated and should contain expected admin group '%s'",
			expectedAdminGroup,
		),
	)
}

// ValidateCELBlocksInvalidGroupsViaUpdate tests that CEL validation blocks Auth resources
// with invalid groups. Since Auth is a singleton resource managed by the operator, we test
// CEL validation by attempting to update the existing Auth resource with invalid values.
func (tc *AuthControllerTestCtx) ValidateCELBlocksInvalidGroupsViaUpdate(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	// Test cases for different invalid update scenarios
	testCases := []struct {
		name        string
		transforms  testf.TransformFn
		description string
	}{
		{
			name:        "empty_admin_groups_array",
			transforms:  testf.Transform(`.spec.adminGroups = [] | .spec.allowedGroups = ["valid-allowed-group"]`),
			description: "Empty AdminGroups array should be blocked by CEL validation",
		},
		{
			name:        "system_authenticated_in_admin_groups",
			transforms:  testf.Transform(`.spec.adminGroups = ["valid-admin-group", "%s"] | .spec.allowedGroups = ["valid-allowed-group"]`, systemAuthenticatedGroup),
			description: "system:authenticated in AdminGroups should be blocked by CEL validation",
		},
		{
			name:        "empty_string_in_admin_groups",
			transforms:  testf.Transform(`.spec.adminGroups = ["valid-admin-group", ""] | .spec.allowedGroups = ["valid-allowed-group"]`),
			description: "Empty string in AdminGroups should be blocked by CEL validation",
		},
		{
			name:        "system_authenticated_in_allowed_groups",
			transforms:  testf.Transform(`.spec.adminGroups = ["valid-admin-group"] | .spec.allowedGroups = ["valid-allowed-group", "%s"]`, systemAuthenticatedGroup),
			description: "system:authenticated in AllowedGroups should be blocked by CEL validation",
		},
		{
			name:        "empty_string_in_allowed_groups",
			transforms:  testf.Transform(`.spec.adminGroups = ["valid-admin-group"] | .spec.allowedGroups = ["valid-allowed-group", ""]`),
			description: "Empty string in AllowedGroups should be blocked by CEL validation",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.Auth, tc.AuthNamespacedName),
				WithMutateFunc(testCase.transforms),
				WithAcceptableErr(k8serr.IsInvalid, "IsInvalid"),
			)
		})
	}
}

// ValidateCELAllowsValidGroups tests that CEL validation allows Auth resources with valid groups.
// Since Auth resources must be named "auth", we test by updating the existing resource.
func (tc *AuthControllerTestCtx) ValidateCELAllowsValidGroups(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	testCases := []struct {
		name        string
		transforms  testf.TransformFn
		description string
	}{
		{
			name:        "valid_groups_only",
			transforms:  testf.Transform(`.spec.adminGroups = ["valid-admin-group-1", "valid-admin-group-2"] | .spec.allowedGroups = ["valid-allowed-group-1", "valid-allowed-group-2"]`),
			description: "Valid groups should be allowed by CEL validation",
		},
		{
			name:        "multiple_allowed_groups",
			transforms:  testf.Transform(`.spec.adminGroups = ["valid-admin-group"] | .spec.allowedGroups = ["valid-allowed-group", "another-valid-group"]`),
			description: "Multiple allowed groups should be allowed by CEL validation",
		},
		{
			name:        "empty_allowed_groups_array",
			transforms:  testf.Transform(`.spec.adminGroups = ["valid-admin-group"] | .spec.allowedGroups = []`),
			description: "Empty AllowedGroups array should be allowed by CEL validation",
		},
		{
			name:        "single_allowed_group",
			transforms:  testf.Transform(`.spec.adminGroups = ["valid-admin-group"] | .spec.allowedGroups = ["single-valid-group"]`),
			description: "Single allowed group should be allowed by CEL validation",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.Auth, tc.AuthNamespacedName),
				WithMutateFunc(testCase.transforms),
				WithCustomErrorMsg(testCase.description),
			)
		})
	}

	// Reset Auth to defaults after CEL tests to avoid polluting subsequent tests
	tc.resetAuthToDefaults(t)
}

// resetAuthToDefaults resets the Auth CR to its default state after CEL validation tests.
// This ensures subsequent tests within the same suite start with predictable Auth state.
func (tc *AuthControllerTestCtx) resetAuthToDefaults(t *testing.T) {
	t.Helper()

	expectedAdminGroup := tc.getExpectedAdminGroupForPlatform()
	expectedAllowedGroup := tc.getExpectedAllowedGroupForPlatform()

	// Reset Auth to default values (within-suite cleanup)
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.Auth, tc.AuthNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.adminGroups = ["%s"] | .spec.allowedGroups = ["%s"]`, expectedAdminGroup, expectedAllowedGroup)),
		WithCustomErrorMsg("Failed to reset Auth CR to default state after CEL tests"),
	)
}

// getExpectedAdminGroupForPlatform returns the expected admin group name based on the current platform.
func (tc *AuthControllerTestCtx) getExpectedAdminGroupForPlatform() string {
	platform := tc.FetchPlatformRelease()
	switch platform {
	case cluster.SelfManagedRhoai:
		return rhodsAdminsName
	case cluster.ManagedRhoai:
		return dedicatedAdminsName
	case cluster.OpenDataHub:
		return odhAdminsName
	default:
		return odhAdminsName // Default fallback
	}
}

// getExpectedAllowedGroupForPlatform returns the expected allowed group name based on the current platform.
func (tc *AuthControllerTestCtx) getExpectedAllowedGroupForPlatform() string {
	platform := tc.FetchPlatformRelease()
	switch platform {
	case cluster.SelfManagedRhoai:
		return rhodsUsersGroupName
	case cluster.ManagedRhoai:
		return rhodsUsersGroupName
	case cluster.OpenDataHub:
		return odhUsersGroupName
	default:
		return odhUsersGroupName // Default fallback
	}
}

// It supports different validation patterns through optional ResourceOpts.
func (tc *AuthControllerTestCtx) validateRBACResource(groupVersionKind schema.GroupVersionKind, name string, opts ...ResourceOpts) {
	nn := types.NamespacedName{Name: name}
	// Only namespace-scoped RBAC resources (Role/RoleBinding) need a namespace
	if groupVersionKind.Kind == "Role" || groupVersionKind.Kind == "RoleBinding" {
		nn.Namespace = tc.AppsNamespace
	}

	// Default options for basic existence validation
	defaultOpts := make([]ResourceOpts, 0, 2+len(opts))
	defaultOpts = append(defaultOpts,
		WithMinimalObject(groupVersionKind, nn),
		WithCustomErrorMsg("Expected %s %s to be created as part of Auth system initialization", groupVersionKind.Kind, name),
	)

	// Merge with provided options (provided options override defaults)
	defaultOpts = append(defaultOpts, opts...)
	tc.EnsureResourceExists(defaultOpts...)
}
