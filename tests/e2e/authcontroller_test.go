package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	// Default admin group names for different platforms.
	rhodsAdminsName = "rhods-admins"
	odhAdminsName   = "odh-admins"

	// Role names used for RBAC configuration.
	adminGroupRoleName   = "admingroup-role"
	allowedGroupRoleName = "allowedgroup-role"

	// RoleBinding names to bind roles to specific groups.
	adminGroupRoleBindingName   = "admingroup-rolebinding"
	allowedGroupRoleBindingName = "allowedgroup-rolebinding"

	// ClusterRole and ClusterRoleBinding names for admin group access at cluster level.
	adminGroupClusterRoleName        = "admingroupcluster-role"
	adminGroupClusterRoleBindingName = "admingroupcluster-rolebinding"
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
		AuthNamespacedName: types.NamespacedName{
			Name:      serviceApi.AuthInstanceName,
			Namespace: tc.AppsNamespace,
		},
	}

	// Define test cases.
	testCases := []TestCase{
		{"Validate Auth CR creation", authCtx.ValidateAuthCRCreation},
		{"Validate Auth CR default content", authCtx.ValidateAuthCRDefaultContent},
		{"Validate Auth Role creation", authCtx.ValidateAuthCRRoleCreation},
		{"Validate Auth RoleBinding creation", authCtx.ValidateAuthCRRoleBindingCreation},
		{"Validate addition of RoleBinding when group is added", authCtx.ValidateAddingGroups},
		{"Validate addition of ClusterRole when group is added", authCtx.ValidateAuthCRClusterRoleCreation},
		{"Validate addition of ClusterRoleBinding when group is added", authCtx.ValidateAuthCRClusterRoleBindingCreation},
		{"Validate removal of bindings when a group is removed", authCtx.ValidateRemovingGroups},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateAuthCRCreation ensures that the Auth CR is created.
func (tc *AuthControllerTestCtx) ValidateAuthCRCreation(t *testing.T) {
	t.Helper()

	// Ensure that exactly one Auth CR exists.
	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Auth, tc.AuthNamespacedName),
		WithCondition(HaveLen(1)),
		WithCustomErrorMsg(
			"Expected exactly one resource '%s' of kind '%s', but found a different number of resources.",
			resources.FormatNamespacedName(tc.AuthNamespacedName), gvk.Auth.Kind,
		),
	)
}

// ValidateAuthCRDefaultContent validates the default content of the Auth CR.
func (tc *AuthControllerTestCtx) ValidateAuthCRDefaultContent(t *testing.T) {
	t.Helper()

	// Get the expected values.
	var expectedAdminGroup string
	expectedAllowedGroup := "system:authenticated"

	// Fetching the Platform release name
	platform := tc.FetchPlatformRelease()

	// Determine expected admin group based on platform.
	switch platform {
	case cluster.SelfManagedRhoai:
		expectedAdminGroup = rhodsAdminsName
	case cluster.ManagedRhoai:
		expectedAdminGroup = "dedicated-admins"
	case cluster.OpenDataHub:
		expectedAdminGroup = odhAdminsName
	}

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Auth, tc.AuthNamespacedName),
		WithCondition(And(
			jq.Match(`.spec.adminGroups | length > 0 and .[0] == "%s"`, expectedAdminGroup),
			jq.Match(`.spec.allowedGroups | length > 0 and .[0] == "%s"`, expectedAllowedGroup),
		)),
		WithCustomErrorMsg(
			"Expected resource '%s' to have at least one entry in 'adminGroups' (first value: '%s') and 'allowedGroups' (first value: '%s')",
			gvk.Auth.Kind,
			expectedAdminGroup,
			expectedAllowedGroup,
		),
	)
}

// ValidateAuthCRRoleCreation validates the creation of the roles for the Auth CR.
func (tc *AuthControllerTestCtx) ValidateAuthCRRoleCreation(t *testing.T) {
	t.Helper()

	// Validate the role for admin and allowed groups.
	roles := []string{adminGroupRoleName, allowedGroupRoleName}
	for _, roleName := range roles {
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.Role, types.NamespacedName{Namespace: tc.AppsNamespace, Name: roleName}),
			WithCustomErrorMsg("Expected admin Role %s to be created", roleName),
		)
	}
}

// ValidateAuthCRClusterRoleCreation validates the creation of the cluster role.
func (tc *AuthControllerTestCtx) ValidateAuthCRClusterRoleCreation(t *testing.T) {
	t.Helper()

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ClusterRole, types.NamespacedName{Namespace: tc.AppsNamespace, Name: adminGroupClusterRoleName}),
		WithCustomErrorMsg("Expected admin ClusterRole %s to be created", adminGroupClusterRoleName),
	)
}

// ValidateAuthCRRoleBindingCreation validates the creation of the role bindings.
func (tc *AuthControllerTestCtx) ValidateAuthCRRoleBindingCreation(t *testing.T) {
	t.Helper()

	roleBindings := []string{adminGroupRoleBindingName, allowedGroupRoleBindingName}
	for _, roleBinding := range roleBindings {
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.RoleBinding, types.NamespacedName{Namespace: tc.AppsNamespace, Name: roleBinding}),
			WithCustomErrorMsg("Expected admin RoleBinding %s to be created", roleBinding),
		)
	}
}

// ValidateAuthCRClusterRoleBindingCreation validates the creation of the cluster role bindings.
func (tc *AuthControllerTestCtx) ValidateAuthCRClusterRoleBindingCreation(t *testing.T) {
	t.Helper()

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ClusterRoleBinding, types.NamespacedName{Namespace: tc.AppsNamespace, Name: adminGroupClusterRoleBindingName}),
		WithCustomErrorMsg("Expected admin ClusterRoleBinding %s to be created", adminGroupClusterRoleBindingName),
	)
}

// ValidateAddingGroups adds groups and validates.
func (tc *AuthControllerTestCtx) ValidateAddingGroups(t *testing.T) {
	t.Helper()

	testAdminGroup := "aTestAdminGroup"
	testAllowedGroup := "aTestAllowedGroup"

	// Update the Auth CR with new admin and allowed groups.
	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Auth, tc.AuthNamespacedName),
		WithMutateFunc(
			testf.Transform(
				`.spec.adminGroups |= . + ["%s"] | .spec.allowedGroups |= . + ["%s"]`, testAdminGroup, testAllowedGroup,
			),
		),
	)

	// Helper function to validate the role and cluster role bindings.
	validateBinding := func(gvk schema.GroupVersionKind, bindingName, groupName string) {
		tc.EnsureResourceExists(
			WithMinimalObject(gvk, types.NamespacedName{Namespace: tc.AppsNamespace, Name: bindingName}),
			WithCondition(jq.Match(`.subjects[1].name == "%s"`, groupName)),
		)
	}

	// Validate the RoleBinding and ClusterRoleBinding for admin and allowed groups.
	validateBinding(gvk.RoleBinding, adminGroupRoleBindingName, testAdminGroup)
	validateBinding(gvk.ClusterRoleBinding, adminGroupClusterRoleBindingName, testAdminGroup)
	validateBinding(gvk.RoleBinding, allowedGroupRoleBindingName, testAllowedGroup)
}

// ValidateRemovingGroups removes groups from Auth CR and validates the changes.
func (tc *AuthControllerTestCtx) ValidateRemovingGroups(t *testing.T) {
	t.Helper()

	// Fetching the Platform release name
	platform := tc.FetchPlatformRelease()

	expectedGroup := odhAdminsName

	if platform == cluster.ManagedRhoai || platform == cluster.SelfManagedRhoai {
		expectedGroup = rhodsAdminsName
	}

	// Update the Auth CR to set only the expected admin group.
	tc.EnsureResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Auth, tc.AuthNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.adminGroups = ["%s"]`, expectedGroup)),
		WithCustomErrorMsg("Failed to create or update Auth resource '%s' with admin group '%s'", serviceApi.AuthInstanceName, expectedGroup),
	)

	// Helper function to validate binding conditions after removal.
	validateBinding := func(bindingType schema.GroupVersionKind, bindingName string, args ...any) {
		tc.EnsureResourceExists(
			WithMinimalObject(bindingType, types.NamespacedName{Namespace: tc.AppsNamespace, Name: bindingName}),
			WithCondition(And(
				jq.Match(`.subjects | length == 1`),
				jq.Match(`.subjects[0].name == "%s"`, expectedGroup),
			)),
			WithCustomErrorMsg(args...),
		)
	}

	// Validate RoleBinding and ClusterRoleBinding for admin group after removal.
	validateBinding(gvk.RoleBinding, adminGroupRoleBindingName,
		"Expected RoleBinding '%s' to have exactly one subject with name '%s'", adminGroupRoleBindingName, expectedGroup)
	validateBinding(gvk.ClusterRoleBinding, adminGroupClusterRoleBindingName,
		"Expected ClusterRoleBinding '%s' to have exactly one subject with name '%s'", adminGroupClusterRoleBindingName, expectedGroup)
}
