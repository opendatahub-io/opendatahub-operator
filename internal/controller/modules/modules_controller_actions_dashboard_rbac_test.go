//nolint:testpackage // Needs package access for unexported functions and registry helpers.
package modules

import (
	"context"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
)

const testModelRegistryNS = "rhoai-model-registries"

func newDashboardRBACTestRR(t *testing.T, platform common.Platform, dsc *dscv2.DataScienceCluster, objects ...client.Object) *odhtypes.ReconciliationRequest {
	t.Helper()

	cli, err := fakeclient.New(fakeclient.WithObjects(objects...))
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}

	if dsc == nil {
		dsc = &dscv2.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-dsc"}}
	}

	return &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dsc,
		Release:  common.Release{Name: platform},
	}
}

// getRoleAndBinding asserts that a Role and RoleBinding both exist in the fake
// client for the given name and namespace, failing the test if either is missing.
func getRoleAndBinding(t *testing.T, rr *odhtypes.ReconciliationRequest, name, namespace string) {
	t.Helper()

	role := &rbacv1.Role{}
	if err := rr.Client.Get(context.Background(), k8stypes.NamespacedName{Name: name, Namespace: namespace}, role); err != nil {
		t.Errorf("expected Role %s/%s to exist: %v", namespace, name, err)
	}

	rb := &rbacv1.RoleBinding{}
	if err := rr.Client.Get(context.Background(), k8stypes.NamespacedName{Name: name, Namespace: namespace}, rb); err != nil {
		t.Errorf("expected RoleBinding %s/%s to exist: %v", namespace, name, err)
	}
}

// assertRoleAndBindingAbsent checks that neither a Role nor RoleBinding exists
// for the given name and namespace.
func assertRoleAndBindingAbsent(t *testing.T, rr *odhtypes.ReconciliationRequest, name, namespace string) {
	t.Helper()

	role := &rbacv1.Role{}
	if err := rr.Client.Get(context.Background(), k8stypes.NamespacedName{Name: name, Namespace: namespace}, role); err == nil {
		t.Errorf("expected Role %s/%s to be absent, but it exists", namespace, name)
	}

	rb := &rbacv1.RoleBinding{}
	if err := rr.Client.Get(context.Background(), k8stypes.NamespacedName{Name: name, Namespace: namespace}, rb); err == nil {
		t.Errorf("expected RoleBinding %s/%s to be absent, but it exists", namespace, name)
	}
}

func enableDashboardInRegistry(t *testing.T) {
	t.Helper()
	withTestRegistry(t)
	DefaultRegistry().Add(&dashboardStub{enabled: true})
}

func enableWorkbenchesInRegistry(t *testing.T) {
	t.Helper()
	DefaultRegistry().Add(&workbenchesStub{enabled: true})
}

type dashboardStub struct {
	BaseHandler

	enabled bool
}

func (s *dashboardStub) GetName() string {
	return componentApi.DashboardComponentName
}
func (s *dashboardStub) IsEnabled(_ *PlatformContext) bool {
	return s.enabled
}
func (s *dashboardStub) BuildModuleCR(_ context.Context, _ client.Client, _ *PlatformContext) (*unstructured.Unstructured, error) {
	return nil, nil
}

type workbenchesStub struct {
	BaseHandler

	enabled bool
}

func (s *workbenchesStub) GetName() string {
	return componentApi.WorkbenchesComponentName
}
func (s *workbenchesStub) IsEnabled(_ *PlatformContext) bool {
	return s.enabled
}
func (s *workbenchesStub) BuildModuleCR(_ context.Context, _ client.Client, _ *PlatformContext) (*unstructured.Unstructured, error) {
	return nil, nil
}

func TestEnsureDashboardNamespacedRBAC_BothNamespacesExist(t *testing.T) {
	enableDashboardInRegistry(t)
	enableWorkbenchesInRegistry(t)

	notebooksNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: cluster.DefaultNotebooksNamespaceRHOAI}}
	modelRegNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testModelRegistryNS}}
	mr := &componentApi.ModelRegistry{
		ObjectMeta: metav1.ObjectMeta{Name: componentApi.ModelRegistryInstanceName},
		Spec: componentApi.ModelRegistrySpec{
			ModelRegistryCommonSpec: componentApi.ModelRegistryCommonSpec{
				RegistriesNamespace: testModelRegistryNS,
			},
		},
	}

	rr := newDashboardRBACTestRR(t, cluster.SelfManagedRhoai, nil, notebooksNS, modelRegNS, mr)

	if err := ensureDashboardNamespacedRBAC(context.Background(), rr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	getRoleAndBinding(t, rr, "rhods-dashboard-notebooks", cluster.DefaultNotebooksNamespaceRHOAI)
	getRoleAndBinding(t, rr, "rhods-dashboard-model-registries", testModelRegistryNS)
}

func TestEnsureDashboardNamespacedRBAC_DashboardDisabled(t *testing.T) {
	withTestRegistry(t)
	DefaultRegistry().Add(&dashboardStub{enabled: false})

	rr := newDashboardRBACTestRR(t, cluster.SelfManagedRhoai, nil)

	if err := ensureDashboardNamespacedRBAC(context.Background(), rr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertRoleAndBindingAbsent(t, rr, "rhods-dashboard-notebooks", cluster.DefaultNotebooksNamespaceRHOAI)
	assertRoleAndBindingAbsent(t, rr, "rhods-dashboard-model-registries", testModelRegistryNS)
}

func TestEnsureDashboardNamespacedRBAC_NotebooksMissing(t *testing.T) {
	enableDashboardInRegistry(t)
	enableWorkbenchesInRegistry(t)

	modelRegNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testModelRegistryNS}}
	mr := &componentApi.ModelRegistry{
		ObjectMeta: metav1.ObjectMeta{Name: componentApi.ModelRegistryInstanceName},
		Spec: componentApi.ModelRegistrySpec{
			ModelRegistryCommonSpec: componentApi.ModelRegistryCommonSpec{
				RegistriesNamespace: testModelRegistryNS,
			},
		},
	}

	rr := newDashboardRBACTestRR(t, cluster.SelfManagedRhoai, nil, modelRegNS, mr)

	if err := ensureDashboardNamespacedRBAC(context.Background(), rr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// notebooks namespace doesn't exist — no RBAC there
	assertRoleAndBindingAbsent(t, rr, "rhods-dashboard-notebooks", cluster.DefaultNotebooksNamespaceRHOAI)
	// model-registry namespace exists — RBAC should be created
	getRoleAndBinding(t, rr, "rhods-dashboard-model-registries", testModelRegistryNS)
}

func TestEnsureDashboardNamespacedRBAC_ModelRegistryMissing(t *testing.T) {
	enableDashboardInRegistry(t)
	enableWorkbenchesInRegistry(t)

	notebooksNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: cluster.DefaultNotebooksNamespaceRHOAI}}

	rr := newDashboardRBACTestRR(t, cluster.SelfManagedRhoai, nil, notebooksNS)

	if err := ensureDashboardNamespacedRBAC(context.Background(), rr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// notebooks namespace exists — RBAC should be created
	getRoleAndBinding(t, rr, "rhods-dashboard-notebooks", cluster.DefaultNotebooksNamespaceRHOAI)
	// no ModelRegistry CR — no model-registry RBAC
	assertRoleAndBindingAbsent(t, rr, "rhods-dashboard-model-registries", testModelRegistryNS)
}

func TestEnsureDashboardNamespacedRBAC_WorkbenchesDisabled(t *testing.T) {
	enableDashboardInRegistry(t)
	DefaultRegistry().Add(&workbenchesStub{enabled: false})

	rr := newDashboardRBACTestRR(t, cluster.SelfManagedRhoai, nil)

	if err := ensureDashboardNamespacedRBAC(context.Background(), rr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertRoleAndBindingAbsent(t, rr, "rhods-dashboard-notebooks", cluster.DefaultNotebooksNamespaceRHOAI)
}

func TestEnsureDashboardNamespacedRBAC_ODHSAName(t *testing.T) {
	enableDashboardInRegistry(t)
	enableWorkbenchesInRegistry(t)

	notebooksNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: cluster.DefaultNotebooksNamespaceODH}}

	rr := newDashboardRBACTestRR(t, cluster.OpenDataHub, nil, notebooksNS)

	if err := ensureDashboardNamespacedRBAC(context.Background(), rr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// "odh-dashboard-notebooks" is the expected name: SA prefix is "odh-dashboard", suffix is "notebooks"
	getRoleAndBinding(t, rr, "odh-dashboard-notebooks", cluster.DefaultNotebooksNamespaceODH)
}

func TestDashboardModelRegistryRBACRules_ContainsCreateVerb(t *testing.T) {
	rules := dashboardModelRegistryRBACRules()

	for _, rule := range rules {
		if len(rule.Resources) == 1 && rule.Resources[0] == rbacResourceSecrets {
			if !slices.Contains(rule.Verbs, rbacVerbCreate) {
				t.Error("model-registry secrets rule missing 'create' verb")
			}
			if !slices.Contains(rule.Verbs, rbacVerbGet) {
				t.Error("model-registry secrets rule missing 'get' verb")
			}
			return
		}
	}
	t.Error("no secrets rule found in model-registry RBAC rules")
}

func TestEnsureDashboardNamespacedRBAC_RoleBindingSubject(t *testing.T) {
	enableDashboardInRegistry(t)
	enableWorkbenchesInRegistry(t)

	notebooksNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: cluster.DefaultNotebooksNamespaceRHOAI}}

	rr := newDashboardRBACTestRR(t, cluster.SelfManagedRhoai, nil, notebooksNS)

	if err := ensureDashboardNamespacedRBAC(context.Background(), rr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rb := &rbacv1.RoleBinding{}
	if err := rr.Client.Get(context.Background(), k8stypes.NamespacedName{
		Name:      "rhods-dashboard-notebooks",
		Namespace: cluster.DefaultNotebooksNamespaceRHOAI,
	}, rb); err != nil {
		t.Fatalf("expected RoleBinding to exist: %v", err)
	}

	if len(rb.Subjects) != 1 {
		t.Fatalf("expected exactly 1 subject in RoleBinding, got %d", len(rb.Subjects))
	}

	subj := rb.Subjects[0]
	if subj.Kind != rbacv1.ServiceAccountKind {
		t.Errorf("expected ServiceAccount kind, got %q", subj.Kind)
	}
	if subj.Name != dashboardSANameRHOAI {
		t.Errorf("expected SA name %q, got %q", dashboardSANameRHOAI, subj.Name)
	}
}

func TestEnsureDashboardNamespacedRBAC_CustomWorkbenchNamespace(t *testing.T) {
	enableDashboardInRegistry(t)
	enableWorkbenchesInRegistry(t)

	customNS := "custom-notebooks"
	notebooksNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: customNS}}

	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dsc"},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Workbenches: componentApi.DSCWorkbenches{
					WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{
						WorkbenchNamespace: customNS,
					},
				},
			},
		},
	}

	rr := newDashboardRBACTestRR(t, cluster.SelfManagedRhoai, dsc, notebooksNS)

	if err := ensureDashboardNamespacedRBAC(context.Background(), rr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// RBAC should land in the custom namespace, not the default one
	getRoleAndBinding(t, rr, "rhods-dashboard-notebooks", customNS)
	assertRoleAndBindingAbsent(t, rr, "rhods-dashboard-notebooks", cluster.DefaultNotebooksNamespaceRHOAI)
}

// TestEnsureDashboardNamespacedRBAC_ModelRegistryEmptyNamespace verifies that when the
// ModelRegistry CR exists but RegistriesNamespace is empty, no model-registry RBAC is created.
func TestEnsureDashboardNamespacedRBAC_ModelRegistryEmptyNamespace(t *testing.T) {
	enableDashboardInRegistry(t)
	enableWorkbenchesInRegistry(t)

	notebooksNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: cluster.DefaultNotebooksNamespaceRHOAI}}
	mr := &componentApi.ModelRegistry{
		ObjectMeta: metav1.ObjectMeta{Name: componentApi.ModelRegistryInstanceName},
		Spec: componentApi.ModelRegistrySpec{
			ModelRegistryCommonSpec: componentApi.ModelRegistryCommonSpec{
				RegistriesNamespace: "", // explicitly empty
			},
		},
	}

	rr := newDashboardRBACTestRR(t, cluster.SelfManagedRhoai, nil, notebooksNS, mr)

	if err := ensureDashboardNamespacedRBAC(context.Background(), rr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	getRoleAndBinding(t, rr, "rhods-dashboard-notebooks", cluster.DefaultNotebooksNamespaceRHOAI)
	assertRoleAndBindingAbsent(t, rr, "rhods-dashboard-model-registries", testModelRegistryNS)
}

// TestEnsureDashboardNamespacedRBAC_ModelRegistryEmptyNamespaceCleansUpStale verifies that
// when RegistriesNamespace is cleared on an existing ModelRegistry CR, stale RBAC is deleted.
func TestEnsureDashboardNamespacedRBAC_ModelRegistryEmptyNamespaceCleansUpStale(t *testing.T) {
	enableDashboardInRegistry(t)
	enableWorkbenchesInRegistry(t)

	mrNS := testModelRegistryNS
	staleNSObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: mrNS}}
	staleRole, staleRB := makeStaleRBACObjects(mrNS, "rhods-dashboard-model-registries")

	// ModelRegistry CR exists but RegistriesNamespace is now empty
	mr := &componentApi.ModelRegistry{
		ObjectMeta: metav1.ObjectMeta{Name: componentApi.ModelRegistryInstanceName},
		Spec: componentApi.ModelRegistrySpec{
			ModelRegistryCommonSpec: componentApi.ModelRegistryCommonSpec{
				RegistriesNamespace: "",
			},
		},
	}

	rr := newDashboardRBACTestRR(t, cluster.SelfManagedRhoai, nil, staleNSObj, staleRole, staleRB, mr)

	if err := ensureDashboardNamespacedRBAC(context.Background(), rr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertRoleAndBindingAbsent(t, rr, "rhods-dashboard-model-registries", mrNS)
}

// TestEnsureDashboardNamespacedRBAC_WorkbenchesDisabledCleansUpStale verifies that when
// Workbenches is disabled, stale notebooks RBAC from a previous reconcile is deleted.
func TestEnsureDashboardNamespacedRBAC_WorkbenchesDisabledCleansUpStale(t *testing.T) {
	enableDashboardInRegistry(t)
	DefaultRegistry().Add(&workbenchesStub{enabled: false})
	DefaultRegistry().Disable(componentApi.WorkbenchesComponentName)

	staleNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: cluster.DefaultNotebooksNamespaceRHOAI}}
	staleRole, staleRB := makeStaleRBACObjects(cluster.DefaultNotebooksNamespaceRHOAI, "rhods-dashboard-notebooks")

	rr := newDashboardRBACTestRR(t, cluster.SelfManagedRhoai, nil, staleNS, staleRole, staleRB)

	if err := ensureDashboardNamespacedRBAC(context.Background(), rr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertRoleAndBindingAbsent(t, rr, "rhods-dashboard-notebooks", cluster.DefaultNotebooksNamespaceRHOAI)
}

// TestEnsureDashboardNamespacedRBAC_Idempotent verifies that running reconcile twice
// with the same state produces no error (SSA makes it safe to re-apply).
func TestEnsureDashboardNamespacedRBAC_Idempotent(t *testing.T) {
	enableDashboardInRegistry(t)
	enableWorkbenchesInRegistry(t)

	notebooksNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: cluster.DefaultNotebooksNamespaceRHOAI}}
	rr := newDashboardRBACTestRR(t, cluster.SelfManagedRhoai, nil, notebooksNS)

	for i := range 2 {
		if err := ensureDashboardNamespacedRBAC(context.Background(), rr); err != nil {
			t.Fatalf("reconcile %d: unexpected error: %v", i+1, err)
		}
	}

	getRoleAndBinding(t, rr, "rhods-dashboard-notebooks", cluster.DefaultNotebooksNamespaceRHOAI)
}

// TestEnsureDashboardNamespacedRBAC_NotebooksRBACRules verifies the notebooks rules
// grant exactly the expected resources and verbs (no extras allowed — guards least-privilege).
func TestEnsureDashboardNamespacedRBAC_NotebooksRBACRules(t *testing.T) {
	rules := dashboardNotebooksRBACRules()

	type expected struct {
		resource string
		verbs    []string
		apiGroup string
	}
	wants := []expected{
		{rbacResourcePVCs, []string{rbacVerbCreate, rbacVerbGet}, ""},
		{rbacResourceCMs, []string{rbacVerbCreate, rbacVerbGet, rbacVerbUpdate}, ""},
		{rbacResourceSecrets, []string{rbacVerbCreate, rbacVerbGet, rbacVerbUpdate}, ""},
	}

	if len(rules) != len(wants) {
		t.Fatalf("expected %d notebook rules, got %d", len(wants), len(rules))
	}
	for i, w := range wants {
		if len(rules[i].Resources) != 1 || rules[i].Resources[0] != w.resource {
			t.Errorf("rule %d: expected resource %q, got %v", i, w.resource, rules[i].Resources)
		}
		if len(rules[i].APIGroups) != 1 || rules[i].APIGroups[0] != w.apiGroup {
			t.Errorf("rule %d (%s): expected APIGroups [%q], got %v", i, w.resource, w.apiGroup, rules[i].APIGroups)
		}
		// Exact verb match: missing verbs and extra verbs both fail.
		gotVerbs := slices.Clone(rules[i].Verbs)
		slices.Sort(gotVerbs)
		wantVerbs := slices.Clone(w.verbs)
		slices.Sort(wantVerbs)
		if !slices.Equal(gotVerbs, wantVerbs) {
			t.Errorf("rule %d (%s): expected verbs %v, got %v", i, w.resource, wantVerbs, gotVerbs)
		}
	}
}

// makeStaleRBACObjects returns a labeled Role and RoleBinding in the given namespace,
// simulating objects left over from a previous reconcile.
func makeStaleRBACObjects(namespace, name string) (*rbacv1.Role, *rbacv1.RoleBinding) {
	managedLabels := map[string]string{dashboardManagedRBACLabel: dashboardManagedRBACLabelValue}
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: managedLabels},
	}
	rb := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: managedLabels},
	}
	return role, rb
}

// TestEnsureDashboardNamespacedRBAC_DisabledCleansUpStale verifies that disabling
// the dashboard causes pre-existing labeled Roles/RoleBindings to be deleted.
func TestEnsureDashboardNamespacedRBAC_DisabledCleansUpStale(t *testing.T) {
	withTestRegistry(t)
	DefaultRegistry().Add(&dashboardStub{enabled: false})

	staleNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: cluster.DefaultNotebooksNamespaceRHOAI}}
	staleRole, staleRB := makeStaleRBACObjects(cluster.DefaultNotebooksNamespaceRHOAI, "rhods-dashboard-notebooks")

	rr := newDashboardRBACTestRR(t, cluster.SelfManagedRhoai, nil, staleNS, staleRole, staleRB)

	if err := ensureDashboardNamespacedRBAC(context.Background(), rr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertRoleAndBindingAbsent(t, rr, "rhods-dashboard-notebooks", cluster.DefaultNotebooksNamespaceRHOAI)
}

// TestEnsureDashboardNamespacedRBAC_NamespaceChangeCleansUpOld verifies that when
// the notebooks namespace changes, the old namespace's Role/RoleBinding is deleted.
func TestEnsureDashboardNamespacedRBAC_NamespaceChangeCleansUpOld(t *testing.T) {
	enableDashboardInRegistry(t)
	enableWorkbenchesInRegistry(t)

	oldNS := "rhods-notebooks-old"
	newNS := cluster.DefaultNotebooksNamespaceRHOAI

	oldNSObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: oldNS}}
	newNSObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: newNS}}
	staleRole, staleRB := makeStaleRBACObjects(oldNS, "rhods-dashboard-notebooks")

	rr := newDashboardRBACTestRR(t, cluster.SelfManagedRhoai, nil, oldNSObj, newNSObj, staleRole, staleRB)

	if err := ensureDashboardNamespacedRBAC(context.Background(), rr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Old namespace objects must be gone, new namespace should have RBAC
	assertRoleAndBindingAbsent(t, rr, "rhods-dashboard-notebooks", oldNS)
	getRoleAndBinding(t, rr, "rhods-dashboard-notebooks", newNS)
}

// TestEnsureDashboardNamespacedRBAC_SharedNamespace verifies that when notebooks and
// model-registry resolve to the same namespace, both Role/RoleBinding pairs are created
// (not one overwriting the other).
func TestEnsureDashboardNamespacedRBAC_SharedNamespace(t *testing.T) {
	enableDashboardInRegistry(t)
	enableWorkbenchesInRegistry(t)

	sharedNS := "shared-namespace"
	sharedNSObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: sharedNS}}
	mr := &componentApi.ModelRegistry{
		ObjectMeta: metav1.ObjectMeta{Name: componentApi.ModelRegistryInstanceName},
		Spec: componentApi.ModelRegistrySpec{
			ModelRegistryCommonSpec: componentApi.ModelRegistryCommonSpec{
				RegistriesNamespace: sharedNS,
			},
		},
	}

	dsc := &dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-dsc"},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Workbenches: componentApi.DSCWorkbenches{
					WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{
						WorkbenchNamespace: sharedNS,
					},
				},
			},
		},
	}

	rr := newDashboardRBACTestRR(t, cluster.SelfManagedRhoai, dsc, sharedNSObj, mr)

	if err := ensureDashboardNamespacedRBAC(context.Background(), rr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both roles must exist in the shared namespace.
	getRoleAndBinding(t, rr, "rhods-dashboard-notebooks", sharedNS)
	getRoleAndBinding(t, rr, "rhods-dashboard-model-registries", sharedNS)
}

// TestEnsureDashboardNamespacedRBAC_ModelRegistryRemovedCleansUp verifies that
// removing the ModelRegistry CR causes its namespace's Role/RoleBinding to be deleted.
func TestEnsureDashboardNamespacedRBAC_ModelRegistryRemovedCleansUp(t *testing.T) {
	enableDashboardInRegistry(t)

	mrNS := testModelRegistryNS
	staleNSObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: mrNS}}
	staleRole, staleRB := makeStaleRBACObjects(mrNS, "rhods-dashboard-model-registries")

	// No ModelRegistry CR in the fake client
	rr := newDashboardRBACTestRR(t, cluster.SelfManagedRhoai, nil, staleNSObj, staleRole, staleRB)

	if err := ensureDashboardNamespacedRBAC(context.Background(), rr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertRoleAndBindingAbsent(t, rr, "rhods-dashboard-model-registries", mrNS)
}
