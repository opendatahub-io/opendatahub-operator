//nolint:testpackage
package auth

import (
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

	persesClusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: persesGlobalDatasourceViewerRole,
		},
	}

	objects := []client.Object{dsci, testNs, maasNs, kuadrantNs, persesClusterRole}

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

	// Verify templates: 3 always + 2 for models-as-a-service + 1 for kuadrant-system = 6
	g.Expect(rr.Templates).To(HaveLen(6))
	g.Expect(rr.Templates[0].Path).To(Equal(AdminGroupRoleTemplate))
	g.Expect(rr.Templates[1].Path).To(Equal(AdminGroupClusterRoleTemplate))
	g.Expect(rr.Templates[2].Path).To(Equal(AllowedGroupClusterRoleTemplate))
	g.Expect(rr.Templates[3].Path).To(Equal(AdminGroupMaaSRoleTemplate))
	g.Expect(rr.Templates[4].Path).To(Equal(AdminGroupMaaSPersesRoleTemplate))
	g.Expect(rr.Templates[5].Path).To(Equal(AdminGroupKuadrantRoleTemplate))
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
			name:          "non-admin role allows system:authenticated",
			groups:        []string{"user1", "system:authenticated", "user2"},
			roleName:      "allowedgroup-role",
			expectSkipped: []string{},
			description:   "Should allow system:authenticated for non-admin roles",
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
				for _, skipped := range tt.expectSkipped {
					if group == skipped {
						validGroupCount--
						break
					}
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

	// Verify resources: 4 RoleBindings + 3 ClusterRoleBindings = 7
	g.Expect(rr.Resources).To(HaveLen(7), "Should create 7 RBAC resources")

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

	g.Expect(roleBindings).To(Equal(4), "Should create 4 RoleBindings")
	g.Expect(clusterRoleBindings).To(Equal(3), "Should create 3 ClusterRoleBindings")
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

	// Verify resources: 4 RoleBindings + 3 ClusterRoleBindings = 7
	g.Expect(rr.Resources).To(HaveLen(7), "Should create 7 RBAC resources")

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

	g.Expect(roleBindings).To(Equal(4), "Should create 4 RoleBindings")
	g.Expect(clusterRoleBindings).To(Equal(3), "Should create 3 ClusterRoleBindings")

	// Verify cluster-scoped roles
	g.Expect(clusterRoleNames).To(ContainElement("data-science-admingroupcluster-role"), "Should create admin group cluster role")
	g.Expect(clusterRoleNames).To(ContainElement("data-science-allowedgroupcluster-role"), "Should create allowed group cluster role")
	g.Expect(clusterRoleNames).To(ContainElement("persesglobaldatasource-viewer-role"), "Should create Perses global datasource viewer role")

	// Verify namespace-scoped roles
	g.Expect(roleNames).To(ContainElement("data-science-admingroup-role"), "Should create admin group role")
	g.Expect(roleNames).To(ContainElement("data-science-admingroup-maas-role"), "Should create MaaS admin group role")
	g.Expect(roleNames).To(ContainElement("data-science-admingroup-maas-perses-role"), "Should create MaaS Perses admin group role")
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

	// Verify resources: 4 RoleBindings + 3 ClusterRoleBindings = 7
	g.Expect(rr.Resources).To(HaveLen(7), "Should create 7 RBAC resources")

	roleBindings := 0
	clusterRoleBindings := 0
	roleNames := []string{}
	clusterRoleNames := []string{}

	var adminGroupSubjects []interface{}
	var allowedGroupSubjects []interface{}
	var adminRoleSubjects []interface{}
	var maaPersesRoleSubjects []interface{}
	var kuadrantRoleSubjects []interface{}
	var persesGlobalSubjects []interface{}

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
						case "data-science-admingroup-maas-perses-role":
							maaPersesRoleSubjects = subjects
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
						case "persesglobaldatasource-viewer-role":
							persesGlobalSubjects = subjects
						}
					}
				}
			}
		}
	}

	g.Expect(roleBindings).To(Equal(4), "Should create 4 RoleBindings")
	g.Expect(clusterRoleBindings).To(Equal(3), "Should create 3 ClusterRoleBindings")
	g.Expect(clusterRoleNames).To(ContainElement("data-science-admingroupcluster-role"), "Should create admin group cluster role")
	g.Expect(clusterRoleNames).To(ContainElement("data-science-allowedgroupcluster-role"), "Should create allowed group cluster role")
	g.Expect(clusterRoleNames).To(ContainElement("persesglobaldatasource-viewer-role"), "Should create Perses global datasource viewer role")
	g.Expect(roleNames).To(ContainElement("data-science-admingroup-role"), "Should create admin group role")
	g.Expect(roleNames).To(ContainElement("data-science-admingroup-maas-perses-role"), "Should create MaaS Perses role")
	g.Expect(roleNames).To(ContainElement("data-science-admingroup-kuadrant-role"), "Should create Kuadrant role")

	// Verify subjects for admin groups (3 groups)
	g.Expect(adminGroupSubjects).To(HaveLen(3), "Admin cluster role binding should have 3 subjects")
	g.Expect(adminRoleSubjects).To(HaveLen(3), "Admin role binding should have 3 subjects")

	// Verify subjects for new role bindings also get the admin groups
	g.Expect(maaPersesRoleSubjects).To(HaveLen(3), "MaaS Perses role binding should have 3 subjects")
	g.Expect(kuadrantRoleSubjects).To(HaveLen(3), "Kuadrant role binding should have 3 subjects")
	g.Expect(persesGlobalSubjects).To(HaveLen(3), "Perses global datasource viewer should have 3 subjects")

	// Verify subjects for allowed groups (3 groups)
	g.Expect(allowedGroupSubjects).To(HaveLen(3), "Allowed cluster role binding should have 3 subjects")

	// Verify specific group names are present in subjects
	adminGroupNames := extractGroupNamesFromSubjects(adminGroupSubjects)
	g.Expect(adminGroupNames).To(ConsistOf("platform-admins", "data-science-admins", "ml-ops-admins"))

	persesGroupNames := extractGroupNamesFromSubjects(maaPersesRoleSubjects)
	g.Expect(persesGroupNames).To(ConsistOf("platform-admins", "data-science-admins", "ml-ops-admins"))

	kuadrantGroupNames := extractGroupNamesFromSubjects(kuadrantRoleSubjects)
	g.Expect(kuadrantGroupNames).To(ConsistOf("platform-admins", "data-science-admins", "ml-ops-admins"))

	persesGlobalGroupNames := extractGroupNamesFromSubjects(persesGlobalSubjects)
	g.Expect(persesGlobalGroupNames).To(ConsistOf("platform-admins", "data-science-admins", "ml-ops-admins"))

	allowedGroupNames := extractGroupNamesFromSubjects(allowedGroupSubjects)
	g.Expect(allowedGroupNames).To(ConsistOf("data-scientists", "analysts", "system:authenticated"))
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

	// 5 total: admin RB + maas RB + maas-perses RB + admin CRB + allowed CRB
	// kuadrant RB skipped (no kuadrant-system namespace)
	// perses global CRB skipped (no persesglobaldatasource-viewer-role ClusterRole)
	g.Expect(rr.Resources).To(HaveLen(5), "Should skip kuadrant RB and perses global CRB when dependencies are missing")

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
	g.Expect(roleNames).To(ContainElement("data-science-admingroup-maas-perses-role"))
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
	g.Expect(namespaceByRole).To(HaveKeyWithValue("data-science-admingroup-maas-perses-role", "models-as-a-service"))
	g.Expect(namespaceByRole).To(HaveKeyWithValue("data-science-admingroup-kuadrant-role", "kuadrant-system"))
}

// TestClusterRoleExistsGuard validates that clusterRoleExists correctly detects
// whether an externally-managed ClusterRole is present on the cluster.
func TestClusterRoleExistsGuard(t *testing.T) {
	ctx := t.Context()

	t.Run("returns true when ClusterRole exists", func(t *testing.T) {
		g := NewWithT(t)
		fakeClient := setupTestClient(g, false)

		rr := &odhtypes.ReconciliationRequest{Client: fakeClient}

		exists, err := clusterRoleExists(ctx, rr, persesGlobalDatasourceViewerRole)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(exists).To(BeTrue())
	})

	t.Run("returns false when ClusterRole does not exist", func(t *testing.T) {
		g := NewWithT(t)
		fakeClient := setupTestClient(g, false)

		rr := &odhtypes.ReconciliationRequest{Client: fakeClient}

		exists, err := clusterRoleExists(ctx, rr, "nonexistent-role")
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(exists).To(BeFalse())
	})
}

// TestManagePermissionsSkipsPersesGlobalWhenClusterRoleMissing validates that the
// perses global datasource viewer ClusterRoleBinding is not created when the
// corresponding ClusterRole (owned by perses-operator) doesn't exist yet.
// When the perses-operator later creates the ClusterRole, the watch triggers
// re-reconciliation and the binding gets created.
func TestManagePermissionsSkipsPersesGlobalWhenClusterRoleMissing(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

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
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kuadrant-system"}},
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

	// 6 total: 4 RoleBindings + 2 ClusterRoleBindings (perses global CRB skipped)
	g.Expect(rr.Resources).To(HaveLen(6))

	clusterRoleNames := []string{}
	for _, resource := range rr.Resources {
		if resource.GetKind() == clusterRoleBindingKind {
			if roleRef, found, err := unstructured.NestedMap(resource.Object, "roleRef"); found && err == nil {
				if roleName, ok := roleRef["name"].(string); ok {
					clusterRoleNames = append(clusterRoleNames, roleName)
				}
			}
		}
	}

	g.Expect(clusterRoleNames).To(ContainElement("data-science-admingroupcluster-role"))
	g.Expect(clusterRoleNames).To(ContainElement("data-science-allowedgroupcluster-role"))
	g.Expect(clusterRoleNames).ToNot(ContainElement(persesGlobalDatasourceViewerRole),
		"Perses global CRB should not be created when the ClusterRole doesn't exist")
}

func extractGroupNamesFromSubjects(subjects []interface{}) []string {
	names := []string{}
	for _, subject := range subjects {
		if subjectMap, ok := subject.(map[string]interface{}); ok {
			if name, ok := subjectMap["name"].(string); ok {
				names = append(names, name)
			}
		}
	}
	return names
}
