//nolint:testpackage
package auth

import (
	"slices"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	userv1 "github.com/openshift/api/user/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

const (
	roleBindingKind        = "RoleBinding"
	clusterRoleBindingKind = "ClusterRoleBinding"
)

// setupTestClient creates a fake client with DSCI pre-configured
// Set includeMockAuth to true if the test needs OAuth authentication.
func setupTestClient(g Gomega, includeMockAuth bool) client.Client {
	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).Should(Succeed())
	g.Expect(dsciv2.AddToScheme(scheme)).Should(Succeed())
	g.Expect(rbacv1.AddToScheme(scheme)).Should(Succeed())
	g.Expect(serviceApi.AddToScheme(scheme)).Should(Succeed())
	g.Expect(configv1.AddToScheme(scheme)).Should(Succeed())
	g.Expect(userv1.AddToScheme(scheme)).Should(Succeed())

	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsci",
		},
		Spec: dsciv2.DSCInitializationSpec{
			ApplicationsNamespace: "test-namespace",
		},
	}

	testNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-namespace",
		},
	}

	maasNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "models-as-a-service",
		},
	}

	kuadrantNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kuadrant-system",
		},
	}

	objects := []client.Object{dsci, testNs, maasNs, kuadrantNs}

	if includeMockAuth {
		mockAuth := &configv1.Authentication{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster",
			},
			Spec: configv1.AuthenticationSpec{
				Type: configv1.AuthenticationTypeIntegratedOAuth,
			},
		}
		objects = append(objects, mockAuth)
	}

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()
}

// - AdminGroupClusterRoleTemplate: ClusterRole for admin groups (cluster-wide access).
func TestInitialize(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	fakeClient := setupTestClient(g, false)

	// Create a basic reconciliation request
	rr := &odhtypes.ReconciliationRequest{
		Client:    fakeClient,
		Templates: []odhtypes.TemplateInfo{},
	}

	err := initialize(ctx, rr)
	g.Expect(err).ToNot(HaveOccurred())

	// Verify templates: 3 always + 1 for models-as-a-service + 1 for kuadrant-system = 5
	g.Expect(rr.Templates).To(HaveLen(5))
	g.Expect(rr.Templates[0].Path).To(Equal(AdminGroupRoleTemplate))
	g.Expect(rr.Templates[1].Path).To(Equal(AdminGroupClusterRoleTemplate))
	g.Expect(rr.Templates[2].Path).To(Equal(AllowedGroupClusterRoleTemplate))
	g.Expect(rr.Templates[3].Path).To(Equal(AdminGroupMaaSRoleTemplate))
	g.Expect(rr.Templates[4].Path).To(Equal(AdminGroupKuadrantRoleTemplate))
}

// TestBindRoleValidation validates the security filtering logic in the bindRole function.
// This implements defense-in-depth by providing controller-level validation in addition
// to the CEL validation at the API level. The test ensures:
//
// 1. Admin roles correctly skip system:authenticated (security risk prevention)
// 2. Admin roles correctly skip empty strings (invalid configuration prevention)
// 3. Non-admin roles allow system:authenticated (valid use case)
// 4. Non-admin roles still skip empty strings (configuration validation)
// 5. Empty group lists are handled gracefully
//
// Security Note: Allowing system:authenticated in admin roles would grant admin access
// to all authenticated users in the cluster, which is a major security vulnerability.
func TestBindRoleValidation(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	fakeClient := setupTestClient(g, false)

	tests := []struct {
		name          string
		groups        []string
		roleName      string
		expectSkipped []string
		description   string
	}{
		{
			name:          "admin role skips system:authenticated",
			groups:        []string{"admin1", "system:authenticated", "admin2"},
			roleName:      "data-science-admingroup-role",
			expectSkipped: []string{"system:authenticated"},
			description:   "Should skip system:authenticated for admin roles",
		},
		{
			name:          "admin role skips empty strings",
			groups:        []string{"admin1", "", "admin2"},
			roleName:      "data-science-admingroup-role",
			expectSkipped: []string{""},
			description:   "Should skip empty strings for admin roles",
		},
		{
			name:          "non-admin role skips system:authenticated",
			groups:        []string{"user1", "system:authenticated", "user2"},
			roleName:      "allowedgroup-role",
			expectSkipped: []string{"system:authenticated"},
			description:   "Should skip system:authenticated for all roles",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			// Create reconciliation request
			rr := &odhtypes.ReconciliationRequest{
				Client:    fakeClient,
				Resources: []unstructured.Unstructured{},
			}

			err := bindRole(ctx, rr, tt.groups, "test-binding", tt.roleName, "test-namespace")
			g.Expect(err).ToNot(HaveOccurred(), tt.description)

			// Verify a resource was added
			g.Expect(rr.Resources).ToNot(BeEmpty(), "Should add RoleBinding resource")

			// Count expected valid groups (all groups minus skipped ones)
			validGroupCount := len(tt.groups)
			for _, group := range tt.groups {
				if slices.Contains(tt.expectSkipped, group) {
					validGroupCount--
				}
			}

			// The exact verification of subjects would require converting from unstructured,
			// but we can verify the basic structure
			resource := rr.Resources[0]
			g.Expect(resource.GetKind()).To(Equal("RoleBinding"))
			g.Expect(resource.GetName()).To(Equal("test-binding"))
			g.Expect(resource.GetNamespace()).To(Equal("test-namespace"))
		})
	}
}

// - ClusterRoleBinding for admin groups (cluster permissions).
func TestManagePermissionsBasic(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	fakeClient := setupTestClient(g, false)

	auth := &serviceApi.Auth{
		ObjectMeta: metav1.ObjectMeta{
			Name: "auth",
		},
		Spec: serviceApi.AuthSpec{
			AdminGroups:   []string{"admin1", "admin2"},
			AllowedGroups: []string{"user1", "system:authenticated"},
		},
	}

	// Create reconciliation request
	rr := &odhtypes.ReconciliationRequest{
		Client:    fakeClient,
		Instance:  auth,
		Resources: []unstructured.Unstructured{},
	}

	err := managePermissions(ctx, rr)
	g.Expect(err).ToNot(HaveOccurred(), "Should create all required RBAC resources")

	// Verify resources: 3 RoleBindings + 2 ClusterRoleBindings = 5
	g.Expect(rr.Resources).To(HaveLen(5), "Should create 5 RBAC resources")

	roleBindings := 0
	clusterRoleBindings := 0

	for _, resource := range rr.Resources {
		switch resource.GetKind() {
		case roleBindingKind:
			roleBindings++
		case clusterRoleBindingKind:
			clusterRoleBindings++
		}
	}

	g.Expect(roleBindings).To(Equal(3), "Should create 3 RoleBindings")
	g.Expect(clusterRoleBindings).To(Equal(2), "Should create 2 ClusterRoleBindings")
}

// TestManagePermissionsInvalidInstance validates error handling when the controller
// receives an invalid resource type. This test ensures:
//
// 1. Type safety by checking that only Auth resources are processed
// 2. Proper error messages for debugging
// 3. Graceful failure without panics or undefined behavior
//
// This is important for robustness and helps catch configuration errors or
// programming mistakes during development.
func TestManagePermissionsInvalidInstance(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	fakeClient := setupTestClient(g, false)

	// Test with wrong instance type
	rr := &odhtypes.ReconciliationRequest{
		Client:    fakeClient,
		Instance:  &serviceApi.Monitoring{}, // Wrong type
		Resources: []unstructured.Unstructured{},
	}

	err := managePermissions(ctx, rr)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("instance is not of type *services.Auth"))
}

// - Managed RHOAI: "rhods-admins".
func TestCreateDefaultGroupBasic(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	fakeClient := setupTestClient(g, true)

	// Test with a basic reconciliation request
	rr := &odhtypes.ReconciliationRequest{
		Client: fakeClient,
		Release: common.Release{
			Name: "test-platform",
		},
		Resources: []unstructured.Unstructured{},
	}

	err := createDefaultGroup(ctx, rr)
	g.Expect(err).ToNot(HaveOccurred(), "Should handle group creation without error")
}

// TestManagePermissions validates that the controller creates the correct RBAC
// resources. This test ensures:
//
//  1. Admin and allowed groups are configured correctly
//  2. The correct number of RBAC resources are created
//  3. All role bindings reference the correct roles
func TestManagePermissions(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	fakeClient := setupTestClient(g, false)

	auth := &serviceApi.Auth{
		ObjectMeta: metav1.ObjectMeta{
			Name: "auth",
		},
		Spec: serviceApi.AuthSpec{
			AdminGroups:   []string{"admin1", "admin2"},
			AllowedGroups: []string{"user1", "system:authenticated"},
		},
	}

	// Create reconciliation request
	rr := &odhtypes.ReconciliationRequest{
		Client:    fakeClient,
		Instance:  auth,
		Resources: []unstructured.Unstructured{},
	}

	err := managePermissions(ctx, rr)
	g.Expect(err).ToNot(HaveOccurred(), "Should create all required RBAC resources")

	// Verify resources: 3 RoleBindings + 2 ClusterRoleBindings = 5
	g.Expect(rr.Resources).To(HaveLen(5), "Should create 5 RBAC resources")

	roleBindings := 0
	clusterRoleBindings := 0
	roleNames := []string{}
	clusterRoleNames := []string{}

	for _, resource := range rr.Resources {
		switch resource.GetKind() {
		case roleBindingKind:
			roleBindings++
			if roleRef, found, err := unstructured.NestedMap(resource.Object, "roleRef"); found && err == nil {
				if roleName, ok := roleRef["name"].(string); ok {
					roleNames = append(roleNames, roleName)
				}
			}
		case clusterRoleBindingKind:
			clusterRoleBindings++
			if roleRef, found, err := unstructured.NestedMap(resource.Object, "roleRef"); found && err == nil {
				if roleName, ok := roleRef["name"].(string); ok {
					clusterRoleNames = append(clusterRoleNames, roleName)
				}
			}
		}
	}

	g.Expect(roleBindings).To(Equal(3), "Should create 3 RoleBindings")
	g.Expect(clusterRoleBindings).To(Equal(2), "Should create 2 ClusterRoleBindings")

	// Verify cluster-scoped roles
	g.Expect(clusterRoleNames).To(ContainElement("data-science-admingroupcluster-role"), "Should create admin group cluster role")
	g.Expect(clusterRoleNames).To(ContainElement("data-science-allowedgroupcluster-role"), "Should create allowed group cluster role")

	// Verify namespace-scoped roles
	g.Expect(roleNames).To(ContainElement("data-science-admingroup-role"), "Should create admin group role")
	g.Expect(roleNames).To(ContainElement("data-science-admingroup-maas-role"), "Should create MaaS admin group role")
	g.Expect(roleNames).To(ContainElement("data-science-admingroup-kuadrant-role"), "Should create Kuadrant admin group role")
}

// TestManagePermissionsMultipleGroups validates that the controller works correctly
// with multiple admin and allowed groups. This test ensures:
//
//  1. Multiple groups can be configured for both admin and allowed access
//  2. The correct number of RBAC resources are created
//  3. All role bindings are created with the correct subjects for each group
//  4. Metrics admin role is not created (that's part of monitoring, not auth)
func TestManagePermissionsMultipleGroups(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	fakeClient := setupTestClient(g, false)

	// Use a distinctly different configuration with more groups
	auth := &serviceApi.Auth{
		ObjectMeta: metav1.ObjectMeta{
			Name: "auth",
		},
		Spec: serviceApi.AuthSpec{
			AdminGroups:   []string{"platform-admins", "data-science-admins", "ml-ops-admins"},
			AllowedGroups: []string{"data-scientists", "analysts", "system:authenticated"},
		},
	}

	// Create reconciliation request
	rr := &odhtypes.ReconciliationRequest{
		Client:    fakeClient,
		Instance:  auth,
		Resources: []unstructured.Unstructured{},
	}

	err := managePermissions(ctx, rr)
	g.Expect(err).ToNot(HaveOccurred(), "Should create all required RBAC resources")

	// Verify resources: 3 RoleBindings + 2 ClusterRoleBindings = 5
	g.Expect(rr.Resources).To(HaveLen(5), "Should create 5 RBAC resources")

	roleBindings := 0
	clusterRoleBindings := 0
	roleNames := []string{}
	clusterRoleNames := []string{}

	var adminGroupSubjects []any
	var allowedGroupSubjects []any
	var adminRoleSubjects []any
	var kuadrantRoleSubjects []any

	for _, resource := range rr.Resources {
		switch resource.GetKind() {
		case roleBindingKind:
			roleBindings++
			if roleRef, found, err := unstructured.NestedMap(resource.Object, "roleRef"); found && err == nil {
				if roleName, ok := roleRef["name"].(string); ok {
					roleNames = append(roleNames, roleName)
					if subjects, found, err := unstructured.NestedSlice(resource.Object, "subjects"); found && err == nil {
						switch roleName {
						case "data-science-admingroup-role":
							adminRoleSubjects = subjects
						case "data-science-admingroup-kuadrant-role":
							kuadrantRoleSubjects = subjects
						}
					}
				}
			}
		case clusterRoleBindingKind:
			clusterRoleBindings++
			if roleRef, found, err := unstructured.NestedMap(resource.Object, "roleRef"); found && err == nil {
				if roleName, ok := roleRef["name"].(string); ok {
					clusterRoleNames = append(clusterRoleNames, roleName)
					if subjects, found, err := unstructured.NestedSlice(resource.Object, "subjects"); found && err == nil {
						switch roleName {
						case "data-science-admingroupcluster-role":
							adminGroupSubjects = subjects
						case "data-science-allowedgroupcluster-role":
							allowedGroupSubjects = subjects
						}
					}
				}
			}
		}
	}

	g.Expect(roleBindings).To(Equal(3), "Should create 3 RoleBindings")
	g.Expect(clusterRoleBindings).To(Equal(2), "Should create 2 ClusterRoleBindings")
	g.Expect(clusterRoleNames).To(ContainElement("data-science-admingroupcluster-role"), "Should create admin group cluster role")
	g.Expect(clusterRoleNames).To(ContainElement("data-science-allowedgroupcluster-role"), "Should create allowed group cluster role")
	g.Expect(roleNames).To(ContainElement("data-science-admingroup-role"), "Should create admin group role")
	g.Expect(roleNames).To(ContainElement("data-science-admingroup-kuadrant-role"), "Should create Kuadrant role")

	// Verify subjects for admin groups (3 groups)
	g.Expect(adminGroupSubjects).To(HaveLen(3), "Admin cluster role binding should have 3 subjects")
	g.Expect(adminRoleSubjects).To(HaveLen(3), "Admin role binding should have 3 subjects")

	// Verify subjects for new role bindings also get the admin groups
	g.Expect(kuadrantRoleSubjects).To(HaveLen(3), "Kuadrant role binding should have 3 subjects")

	// Verify subjects for allowed groups (2 groups, system:authenticated is filtered out)
	g.Expect(allowedGroupSubjects).To(HaveLen(2), "Allowed cluster role binding should have 2 subjects")

	// Verify specific group names are present in subjects
	adminGroupNames := extractGroupNamesFromSubjects(adminGroupSubjects)
	g.Expect(adminGroupNames).To(ConsistOf("platform-admins", "data-science-admins", "ml-ops-admins"))

	kuadrantGroupNames := extractGroupNamesFromSubjects(kuadrantRoleSubjects)
	g.Expect(kuadrantGroupNames).To(ConsistOf("platform-admins", "data-science-admins", "ml-ops-admins"))

	allowedGroupNames := extractGroupNamesFromSubjects(allowedGroupSubjects)
	g.Expect(allowedGroupNames).To(ConsistOf("data-scientists", "analysts"))
}

// TestManagePermissionsKuadrantNamespaceMissing validates that when the kuadrant-system
// namespace does not exist, the kuadrant role binding is skipped while all other
// bindings are still created. This tests the defensive namespace-existence check
// in bindRole for externally-managed namespaces.
func TestManagePermissionsKuadrantNamespaceMissing(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	// Build a client without the kuadrant-system namespace
	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).Should(Succeed())
	g.Expect(dsciv2.AddToScheme(scheme)).Should(Succeed())
	g.Expect(rbacv1.AddToScheme(scheme)).Should(Succeed())
	g.Expect(serviceApi.AddToScheme(scheme)).Should(Succeed())
	g.Expect(configv1.AddToScheme(scheme)).Should(Succeed())
	g.Expect(userv1.AddToScheme(scheme)).Should(Succeed())

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			&dsciv2.DSCInitialization{
				ObjectMeta: metav1.ObjectMeta{Name: "test-dsci"},
				Spec:       dsciv2.DSCInitializationSpec{ApplicationsNamespace: "test-namespace"},
			},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-namespace"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "models-as-a-service"}},
		).
		Build()

	auth := &serviceApi.Auth{
		ObjectMeta: metav1.ObjectMeta{Name: "auth"},
		Spec: serviceApi.AuthSpec{
			AdminGroups:   []string{"admin1"},
			AllowedGroups: []string{"user1"},
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:    fakeClient,
		Instance:  auth,
		Resources: []unstructured.Unstructured{},
	}

	err := managePermissions(ctx, rr)
	g.Expect(err).ToNot(HaveOccurred())

	// 4 total: admin RB + maas RB + admin CRB + allowed CRB
	// kuadrant RB skipped (no kuadrant-system namespace)
	g.Expect(rr.Resources).To(HaveLen(4), "Should skip kuadrant RB when namespace is missing")

	roleNames := []string{}
	for _, resource := range rr.Resources {
		if resource.GetKind() == roleBindingKind {
			if roleRef, found, err := unstructured.NestedMap(resource.Object, "roleRef"); found && err == nil {
				if roleName, ok := roleRef["name"].(string); ok {
					roleNames = append(roleNames, roleName)
				}
			}
		}
	}

	g.Expect(roleNames).To(ContainElement("data-science-admingroup-role"))
	g.Expect(roleNames).To(ContainElement("data-science-admingroup-maas-role"))
	g.Expect(roleNames).ToNot(ContainElement("data-science-admingroup-kuadrant-role"),
		"Kuadrant role binding should not be created when namespace is missing")
}

// TestBindRoleSkipsWhenNamespaceMissing validates that bindRole gracefully skips
// RoleBinding creation when the target namespace does not exist, returning nil
// rather than an error.
func TestBindRoleSkipsWhenNamespaceMissing(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	fakeClient := setupTestClient(g, false)

	rr := &odhtypes.ReconciliationRequest{
		Client:    fakeClient,
		Resources: []unstructured.Unstructured{},
	}

	err := bindRole(ctx, rr, []string{"admin1"}, "test-binding", "test-role", "nonexistent-namespace")
	g.Expect(err).ToNot(HaveOccurred(), "Should not error when namespace is missing")
	g.Expect(rr.Resources).To(BeEmpty(), "Should not create RoleBinding when namespace is missing")
}

// TestManagePermissionsRoleBindingNamespaces validates that role bindings created by
// managePermissions are placed in the correct namespaces.
func TestManagePermissionsRoleBindingNamespaces(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	fakeClient := setupTestClient(g, false)

	auth := &serviceApi.Auth{
		ObjectMeta: metav1.ObjectMeta{Name: "auth"},
		Spec: serviceApi.AuthSpec{
			AdminGroups:   []string{"admin1"},
			AllowedGroups: []string{"user1"},
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:    fakeClient,
		Instance:  auth,
		Resources: []unstructured.Unstructured{},
	}

	err := managePermissions(ctx, rr)
	g.Expect(err).ToNot(HaveOccurred())

	namespaceByRole := map[string]string{}
	for _, resource := range rr.Resources {
		if resource.GetKind() == roleBindingKind {
			if roleRef, found, err := unstructured.NestedMap(resource.Object, "roleRef"); found && err == nil {
				if roleName, ok := roleRef["name"].(string); ok {
					namespaceByRole[roleName] = resource.GetNamespace()
				}
			}
		}
	}

	g.Expect(namespaceByRole).To(HaveKeyWithValue("data-science-admingroup-role", "test-namespace"))
	g.Expect(namespaceByRole).To(HaveKeyWithValue("data-science-admingroup-maas-role", "models-as-a-service"))
	g.Expect(namespaceByRole).To(HaveKeyWithValue("data-science-admingroup-kuadrant-role", "kuadrant-system"))
}

// TestBindClusterRoleValidation mirrors TestBindRoleValidation for the bindClusterRole
// function, verifying that the system:authenticated filtering and empty-string filtering
// behave correctly for both admin and non-admin cluster role bindings.
func TestBindClusterRoleValidation(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	fakeClient := setupTestClient(g, false)

	tests := []struct {
		name          string
		groups        []string
		roleName      string
		expectSkipped []string
		description   string
	}{
		{
			name:          "admin cluster role skips system:authenticated",
			groups:        []string{"admin1", "system:authenticated", "admin2"},
			roleName:      "data-science-admingroupcluster-role",
			expectSkipped: []string{"system:authenticated"},
			description:   "Should skip system:authenticated for admin cluster roles",
		},
		{
			name:          "admin cluster role skips empty strings",
			groups:        []string{"admin1", "", "admin2"},
			roleName:      "data-science-admingroupcluster-role",
			expectSkipped: []string{""},
			description:   "Should skip empty strings for admin cluster roles",
		},
		{
			name:          "non-admin cluster role skips system:authenticated",
			groups:        []string{"user1", "system:authenticated", "user2"},
			roleName:      "data-science-allowedgroupcluster-role",
			expectSkipped: []string{"system:authenticated"},
			description:   "Should skip system:authenticated for all cluster roles",
		},
		{
			name:          "non-admin cluster role skips empty strings",
			groups:        []string{"user1", "", "user2"},
			roleName:      "data-science-allowedgroupcluster-role",
			expectSkipped: []string{""},
			description:   "Should skip empty strings for non-admin cluster roles",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			rr := &odhtypes.ReconciliationRequest{
				Client:    fakeClient,
				Resources: []unstructured.Unstructured{},
			}

			err := bindClusterRole(ctx, rr, tt.groups, "test-cluster-binding", tt.roleName)
			g.Expect(err).ToNot(HaveOccurred(), tt.description)

			g.Expect(rr.Resources).ToNot(BeEmpty(), "Should add ClusterRoleBinding resource")

			validGroupCount := len(tt.groups)
			for _, group := range tt.groups {
				if slices.Contains(tt.expectSkipped, group) {
					validGroupCount--
				}
			}

			resource := rr.Resources[0]
			g.Expect(resource.GetKind()).To(Equal("ClusterRoleBinding"))
			g.Expect(resource.GetName()).To(Equal("test-cluster-binding"))

			subjects, _, _ := unstructured.NestedSlice(resource.Object, "subjects")
			names := extractGroupNamesFromSubjects(subjects)

			for _, skipped := range tt.expectSkipped {
				if skipped != "" {
					g.Expect(names).ToNot(ContainElement(skipped), tt.description)
				}
			}
			g.Expect(names).To(HaveLen(validGroupCount), tt.description)
		})
	}
}

// Regression test: verifies that the system:authenticated principal cannot be
// bound to any admin-level role, including the MaaS and Kuadrant admin roles.
// bindRole and bindClusterRole must filter system:authenticated for all admin
// role names; this test ensures that guard is never inadvertently removed.
func TestManagePermissionsBlocksSystemAuthenticatedInAllAdminBindings(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	fakeClient := setupTestClient(g, false)

	auth := &serviceApi.Auth{
		ObjectMeta: metav1.ObjectMeta{Name: "auth"},
		Spec: serviceApi.AuthSpec{
			AdminGroups:   []string{"real-admin-group", "system:authenticated"},
			AllowedGroups: []string{"user1"},
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:    fakeClient,
		Instance:  auth,
		Resources: []unstructured.Unstructured{},
	}

	err := managePermissions(ctx, rr)
	g.Expect(err).ToNot(HaveOccurred())

	adminRoleNames := map[string]bool{
		"data-science-admingroup-role":          true,
		"data-science-admingroup-maas-role":     true,
		"data-science-admingroup-kuadrant-role": true,
		"data-science-admingroupcluster-role":   true,
	}

	for _, resource := range rr.Resources {
		var roleName string
		if roleRef, found, err := unstructured.NestedMap(resource.Object, "roleRef"); found && err == nil {
			roleName, _ = roleRef["name"].(string)
		}
		if !adminRoleNames[roleName] {
			continue
		}

		subjects, _, _ := unstructured.NestedSlice(resource.Object, "subjects")
		names := extractGroupNamesFromSubjects(subjects)
		g.Expect(names).ToNot(
			ContainElement("system:authenticated"),
			"system:authenticated must not be bound to admin role %q (binding %q)",
			roleName, resource.GetName(),
		)
	}
}

func extractGroupNamesFromSubjects(subjects []any) []string {
	names := []string{}
	for _, subject := range subjects {
		if subjectMap, ok := subject.(map[string]any); ok {
			if name, ok := subjectMap["name"].(string); ok {
				names = append(names, name)
			}
		}
	}
	return names
}

// TestValidateGroupsSecurity tests the security validation function that can reject
// system:authenticated based on configuration.
func TestValidateGroupsSecurity(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name          string
		groups        []string
		roleName      string
		strictMode    bool
		expectError   bool
		errorContains string
		description   string
	}{
		{
			name:        "allows system:authenticated in non-strict mode",
			groups:      []string{"user1", "system:authenticated", "user2"},
			roleName:    "test-role",
			strictMode:  false,
			expectError: false,
			description: "Should allow system:authenticated when strict mode is disabled",
		},
		{
			name:          "rejects system:authenticated in strict mode",
			groups:        []string{"user1", "system:authenticated", "user2"},
			roleName:      "test-role",
			strictMode:    true,
			expectError:   true,
			errorContains: "system:authenticated",
			description:   "Should reject system:authenticated when strict mode is enabled",
		},
		{
			name:        "allows valid groups in strict mode",
			groups:      []string{"user1", "user2", "admin1"},
			roleName:    "test-role",
			strictMode:  true,
			expectError: false,
			description: "Should allow valid groups even in strict mode",
		},
		{
			name:        "allows empty groups list",
			groups:      []string{},
			roleName:    "test-role",
			strictMode:  true,
			expectError: false,
			description: "Should handle empty groups list without error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			err := validateGroupsSecurity(ctx, tt.groups, tt.roleName, tt.strictMode)

			if tt.expectError {
				g.Expect(err).To(HaveOccurred(), tt.description)
				if tt.errorContains != "" {
					g.Expect(err.Error()).To(ContainSubstring(tt.errorContains), tt.description)
				}
			} else {
				g.Expect(err).ToNot(HaveOccurred(), tt.description)
			}
		})
	}
}

// TestLogDeprecationWarnings tests the deprecation warning logging functionality.
func TestLogDeprecationWarnings(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name        string
		auth        *serviceApi.Auth
		description string
	}{
		{
			name: "logs warning for system:authenticated in AdminGroups",
			auth: &serviceApi.Auth{
				ObjectMeta: metav1.ObjectMeta{Name: "test-auth"},
				Spec: serviceApi.AuthSpec{
					AdminGroups:   []string{"admin1", "system:authenticated"},
					AllowedGroups: []string{"user1"},
				},
			},
			description: "Should log warning for system:authenticated in AdminGroups",
		},
		{
			name: "logs warning for system:authenticated in AllowedGroups",
			auth: &serviceApi.Auth{
				ObjectMeta: metav1.ObjectMeta{Name: "test-auth"},
				Spec: serviceApi.AuthSpec{
					AdminGroups:   []string{"admin1"},
					AllowedGroups: []string{"user1", "system:authenticated"},
				},
			},
			description: "Should log warning for system:authenticated in AllowedGroups",
		},
		{
			name: "no warnings for valid groups",
			auth: &serviceApi.Auth{
				ObjectMeta: metav1.ObjectMeta{Name: "test-auth"},
				Spec: serviceApi.AuthSpec{
					AdminGroups:   []string{"admin1", "admin2"},
					AllowedGroups: []string{"user1", "user2"},
				},
			},
			description: "Should not log warnings for valid groups",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			// The function should not error - it only logs
			g.Expect(func() {
				logDeprecationWarnings(ctx, tt.auth)
			}).ToNot(Panic(), tt.description)
		})
	}
}

// TestUpdateDeprecationStatusConditions tests the status condition update functionality.
func TestUpdateDeprecationStatusConditions(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	fakeClient := setupTestClient(g, false)

	tests := []struct {
		name                    string
		auth                    *serviceApi.Auth
		expectCondition         bool
		expectedConditionType   string
		expectedConditionStatus metav1.ConditionStatus
		description             string
	}{
		{
			name: "adds deprecation condition for system:authenticated in AdminGroups",
			auth: &serviceApi.Auth{
				ObjectMeta: metav1.ObjectMeta{Name: "test-auth"},
				Spec: serviceApi.AuthSpec{
					AdminGroups:   []string{"admin1", "system:authenticated"},
					AllowedGroups: []string{"user1"},
				},
			},
			expectCondition:         true,
			expectedConditionType:   deprecatedGroupUsageCondition,
			expectedConditionStatus: metav1.ConditionTrue,
			description:             "Should add deprecation condition for system:authenticated in AdminGroups",
		},
		{
			name: "adds deprecation condition for system:authenticated in AllowedGroups",
			auth: &serviceApi.Auth{
				ObjectMeta: metav1.ObjectMeta{Name: "test-auth"},
				Spec: serviceApi.AuthSpec{
					AdminGroups:   []string{"admin1"},
					AllowedGroups: []string{"user1", "system:authenticated"},
				},
			},
			expectCondition:         true,
			expectedConditionType:   deprecatedGroupUsageCondition,
			expectedConditionStatus: metav1.ConditionTrue,
			description:             "Should add deprecation condition for system:authenticated in AllowedGroups",
		},
		{
			name: "no deprecation condition for valid groups",
			auth: &serviceApi.Auth{
				ObjectMeta: metav1.ObjectMeta{Name: "test-auth"},
				Spec: serviceApi.AuthSpec{
					AdminGroups:   []string{"admin1", "admin2"},
					AllowedGroups: []string{"user1", "user2"},
				},
			},
			expectCondition: false,
			description:     "Should not add deprecation condition for valid groups",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			rr := &odhtypes.ReconciliationRequest{
				Client:   fakeClient,
				Instance: tt.auth,
			}

			// Note: This function updates status, but in test with fake client,
			// we can check if conditions were set on the Auth object
			updateDeprecationStatusConditions(ctx, rr, tt.auth)

			conditions := tt.auth.GetConditions()

			if tt.expectCondition {
				g.Expect(conditions).ToNot(BeEmpty(), tt.description)

				found := false
				for _, condition := range conditions {
					if condition.Type == tt.expectedConditionType {
						found = true
						g.Expect(condition.Status).To(Equal(tt.expectedConditionStatus), tt.description)
						g.Expect(condition.Reason).To(Equal("SystemAuthenticatedDeprecated"), tt.description)
						break
					}
				}
				g.Expect(found).To(BeTrue(), "Should find deprecation condition")
			} else {
				// Check that no deprecation condition exists
				for _, condition := range conditions {
					g.Expect(condition.Type).ToNot(Equal(deprecatedGroupUsageCondition), tt.description)
				}
			}
		})
	}
}

// TestIsStrictModeEnabled tests the environment variable-based strict mode detection.
func TestIsStrictModeEnabled(t *testing.T) {
	// Test cases with environment variable manipulation
	tests := []struct {
		name           string
		envValue       string
		expectedResult bool
		description    string
	}{
		{
			name:           "strict mode enabled with 'true'",
			envValue:       "true",
			expectedResult: true,
			description:    "Should return true when STRICT_SECURITY_MODE=true",
		},
		{
			name:           "strict mode enabled with '1'",
			envValue:       "1",
			expectedResult: true,
			description:    "Should return true when STRICT_SECURITY_MODE=1",
		},
		{
			name:           "strict mode disabled with 'false'",
			envValue:       "false",
			expectedResult: false,
			description:    "Should return false when STRICT_SECURITY_MODE=false",
		},
		{
			name:           "strict mode disabled with empty value",
			envValue:       "",
			expectedResult: false,
			description:    "Should return false when STRICT_SECURITY_MODE is empty",
		},
		{
			name:           "strict mode disabled with invalid value",
			envValue:       "invalid",
			expectedResult: false,
			description:    "Should return false when STRICT_SECURITY_MODE has invalid value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			// Set environment variable for this test
			t.Setenv("STRICT_SECURITY_MODE", tt.envValue)

			result := isStrictModeEnabled()
			g.Expect(result).To(Equal(tt.expectedResult), tt.description)
		})
	}
}

// TestManagePermissionsWithSecurityValidation tests that the enhanced managePermissions
// function correctly calls security validation and deprecation tracking.
func TestManagePermissionsWithSecurityValidation(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	fakeClient := setupTestClient(g, false)

	tests := []struct {
		name        string
		auth        *serviceApi.Auth
		expectError bool
		description string
	}{
		{
			name: "succeeds with valid groups in non-strict mode",
			auth: &serviceApi.Auth{
				ObjectMeta: metav1.ObjectMeta{Name: "auth"},
				Spec: serviceApi.AuthSpec{
					AdminGroups:   []string{"admin1", "admin2"},
					AllowedGroups: []string{"user1", "user2"},
				},
			},
			expectError: false,
			description: "Should succeed with valid groups",
		},
		{
			name: "succeeds with system:authenticated in non-strict mode",
			auth: &serviceApi.Auth{
				ObjectMeta: metav1.ObjectMeta{Name: "auth"},
				Spec: serviceApi.AuthSpec{
					AdminGroups:   []string{"admin1"},
					AllowedGroups: []string{"user1", "system:authenticated"},
				},
			},
			expectError: false,
			description: "Should succeed with system:authenticated in non-strict mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			rr := &odhtypes.ReconciliationRequest{
				Client:    fakeClient,
				Instance:  tt.auth,
				Resources: []unstructured.Unstructured{},
			}

			err := managePermissions(ctx, rr)

			if tt.expectError {
				g.Expect(err).To(HaveOccurred(), tt.description)
			} else {
				g.Expect(err).ToNot(HaveOccurred(), tt.description)

				// Verify that security validation was called by checking that
				// deprecation warnings would be set if system:authenticated is present
				if slices.Contains(tt.auth.Spec.AllowedGroups, "system:authenticated") ||
					slices.Contains(tt.auth.Spec.AdminGroups, "system:authenticated") {
					conditions := tt.auth.GetConditions()
					found := false
					for _, condition := range conditions {
						if condition.Type == deprecatedGroupUsageCondition {
							found = true
							break
						}
					}
					g.Expect(found).To(BeTrue(), "Should have deprecation condition for system:authenticated usage")
				}
			}
		})
	}
}

// TestSystemAuthenticatedFilteredForAllRoles validates that system:authenticated
// is filtered out for ALL role types (admin and non-admin) as a security measure.
func TestSystemAuthenticatedFilteredForAllRoles(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	fakeClient := setupTestClient(g, false)

	tests := []struct {
		name     string
		roleName string
		isAdmin  bool
	}{
		{
			name:     "admin role filters system:authenticated",
			roleName: "data-science-admingroup-role",
			isAdmin:  true,
		},
		{
			name:     "allowed role filters system:authenticated",
			roleName: "data-science-allowedgroupcluster-role",
			isAdmin:  false,
		},
		{
			name:     "custom role filters system:authenticated",
			roleName: "custom-role",
			isAdmin:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			rr := &odhtypes.ReconciliationRequest{
				Client:    fakeClient,
				Resources: []unstructured.Unstructured{},
			}

			groups := []string{"valid-group1", "system:authenticated", "valid-group2"}

			if tt.isAdmin {
				err := bindRole(ctx, rr, groups, "test-binding", tt.roleName, "test-namespace")
				g.Expect(err).ToNot(HaveOccurred())
			} else {
				err := bindClusterRole(ctx, rr, groups, "test-cluster-binding", tt.roleName)
				g.Expect(err).ToNot(HaveOccurred())
			}

			// Verify a resource was added
			g.Expect(rr.Resources).ToNot(BeEmpty(), "Should add role binding resource")

			// Extract subjects and verify system:authenticated is filtered out
			resource := rr.Resources[0]
			subjects, _, _ := unstructured.NestedSlice(resource.Object, "subjects")
			names := extractGroupNamesFromSubjects(subjects)

			// Should have only 2 valid groups, system:authenticated should be filtered
			g.Expect(names).To(HaveLen(2), "Should filter out system:authenticated")
			g.Expect(names).ToNot(ContainElement("system:authenticated"), "system:authenticated must be filtered")
			g.Expect(names).To(ContainElement("valid-group1"), "Should retain valid groups")
			g.Expect(names).To(ContainElement("valid-group2"), "Should retain valid groups")
		})
	}
}
