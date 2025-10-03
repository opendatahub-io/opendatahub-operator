package e2e_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
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
			Namespace: "", // Auth is cluster-scoped, no namespace needed
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
		{"Validate CEL blocks Auth update with invalid groups", authCtx.ValidateCELBlocksInvalidGroupsViaUpdate},
		{"Validate CEL allows Auth with valid groups", authCtx.ValidateCELAllowsValidGroups},
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
	tc.EventuallyResourceCreatedOrUpdated(
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
	tc.EventuallyResourceCreatedOrUpdated(
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

// ValidateCELBlocksInvalidGroupsViaUpdate tests that CEL validation blocks Auth resources
// with invalid groups. Since Auth is a singleton resource managed by the operator, we test
// CEL validation by attempting to update the existing Auth resource with invalid values.
func (tc *AuthControllerTestCtx) ValidateCELBlocksInvalidGroupsViaUpdate(t *testing.T) {
	t.Helper()

	// Test cases for different invalid update scenarios
	testCases := []struct {
		name          string
		invalidValue  string
		fieldName     string
		adminGroups   []string
		allowedGroups []string
	}{
		{
			name:          "empty AdminGroups array",
			invalidValue:  "cannot be empty",
			fieldName:     "AdminGroups",
			adminGroups:   []string{},
			allowedGroups: []string{"valid-allowed-group"},
		},
		{
			name:          "system:authenticated in AdminGroups",
			invalidValue:  "cannot contain 'system:authenticated'",
			fieldName:     "AdminGroups",
			adminGroups:   []string{"valid-admin-group", "system:authenticated"},
			allowedGroups: []string{"valid-allowed-group"},
		},
		{
			name:          "empty string in AdminGroups",
			invalidValue:  "cannot contain 'system:authenticated' or empty strings",
			fieldName:     "AdminGroups",
			adminGroups:   []string{"valid-admin-group", ""},
			allowedGroups: []string{"valid-allowed-group"},
		},
		{
			name:          "empty string in AllowedGroups",
			invalidValue:  "cannot contain empty strings",
			fieldName:     "AllowedGroups",
			adminGroups:   []string{"valid-admin-group"},
			allowedGroups: []string{"valid-allowed-group", ""},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// Get the current Auth resource first using the test framework
			authUnstructured := tc.FetchResource(WithMinimalObject(gvk.Auth, tc.AuthNamespacedName))

			// Convert to typed Auth object
			currentAuth := &serviceApi.Auth{}
			err := runtime.DefaultUnstructuredConverter.FromUnstructured(authUnstructured.Object, currentAuth)
			if err != nil {
				t.Fatalf("Failed to convert Auth resource: %v", err)
			}

			// Create a copy and modify it with invalid values
			invalidAuth := currentAuth.DeepCopy()
			invalidAuth.Spec.AdminGroups = testCase.adminGroups
			invalidAuth.Spec.AllowedGroups = testCase.allowedGroups

			// Try to update with invalid values - this should fail
			ctx := t.Context()
			err = tc.Client().Update(ctx, invalidAuth)

			// The update should fail with CEL validation error
			if err == nil {
				t.Fatalf("Expected CEL validation to block update with %s in %s, but update succeeded", testCase.invalidValue, testCase.fieldName)
			}

			// Verify it's the correct type of error
			if !k8serr.IsInvalid(err) {
				t.Fatalf("Expected Invalid error from CEL validation, got: %v (type: BadRequest=%v, Invalid=%v)",
					err, k8serr.IsBadRequest(err), k8serr.IsInvalid(err))
			}

			// Verify error message contains expected content
			errorMsg := err.Error()
			if testCase.fieldName != "" && !strings.Contains(strings.ToLower(errorMsg), strings.ToLower(testCase.fieldName)) {
				t.Errorf("Expected error message to reference field '%s', got: %s", testCase.fieldName, errorMsg)
			}

			if testCase.invalidValue != "" && testCase.invalidValue != "empty string" && !strings.Contains(errorMsg, testCase.invalidValue) {
				t.Errorf("Expected error message to contain invalid value '%s', got: %s", testCase.invalidValue, errorMsg)
			}
		})
	}
}

// ValidateCELAllowsValidGroups tests that CEL validation allows Auth resources with valid groups.
// Since Auth resources must be named "auth", we test by updating the existing resource.
func (tc *AuthControllerTestCtx) ValidateCELAllowsValidGroups(t *testing.T) {
	t.Helper()

	testCases := []struct {
		name        string
		transform   string
		description string
	}{
		{
			name:        "valid groups only",
			transform:   `.spec.adminGroups = ["valid-admin-group-1", "valid-admin-group-2"] | .spec.allowedGroups = ["valid-allowed-group-1", "valid-allowed-group-2"]`,
			description: "Expected Auth resource update with valid groups to be allowed",
		},
		{
			name:        "system:authenticated in AllowedGroups",
			transform:   `.spec.adminGroups = ["valid-admin-group"] | .spec.allowedGroups = ["valid-allowed-group", "system:authenticated"]`,
			description: "Expected Auth resource update with system:authenticated in AllowedGroups to be allowed",
		},
		{
			name:        "empty AllowedGroups array",
			transform:   `.spec.adminGroups = ["valid-admin-group"] | .spec.allowedGroups = []`,
			description: "Expected Auth resource update with empty AllowedGroups array to be allowed",
		},
		{
			name:        "only system:authenticated in AllowedGroups",
			transform:   `.spec.adminGroups = ["valid-admin-group"] | .spec.allowedGroups = ["system:authenticated"]`,
			description: "Expected Auth resource update with only system:authenticated in AllowedGroups to be allowed",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// Test that CEL validation allows this valid update
			tc.EventuallyResourceCreatedOrUpdated(
				WithMinimalObject(gvk.Auth, tc.AuthNamespacedName),
				WithMutateFunc(testf.Transform("%s", testCase.transform)),
				WithCustomErrorMsg(testCase.description),
			)
		})
	}
}
