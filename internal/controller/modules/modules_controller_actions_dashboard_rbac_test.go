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
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
)

const testModelRegistryNS = "rhoai-model-registries"

func newDashboardRBACTestRR(t *testing.T, platform common.Platform, dsc *dscv2.DataScienceCluster, objects ...client.Object) *types.ReconciliationRequest {
	t.Helper()

	cli, err := fakeclient.New(fakeclient.WithObjects(objects...))
	if err != nil {
		t.Fatalf("create fake client: %v", err)
	}

	if dsc == nil {
		dsc = &dscv2.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: "test-dsc"}}
	}

	return &types.ReconciliationRequest{
		Client:   cli,
		Instance: dsc,
		Release:  common.Release{Name: platform},
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

	if len(rr.Resources) != 4 {
		t.Fatalf("expected 4 resources (2 Roles + 2 RoleBindings), got %d", len(rr.Resources))
	}

	hasRole := func(name, namespace string) bool {
		for _, res := range rr.Resources {
			if res.GetKind() == dashboardRoleKind && res.GetName() == name && res.GetNamespace() == namespace {
				return true
			}
		}
		return false
	}
	hasRoleBinding := func(name, namespace string) bool {
		for _, res := range rr.Resources {
			if res.GetKind() == "RoleBinding" && res.GetName() == name && res.GetNamespace() == namespace {
				return true
			}
		}
		return false
	}

	if !hasRole("rhods-dashboard-notebooks", cluster.DefaultNotebooksNamespaceRHOAI) {
		t.Error("missing notebooks Role")
	}
	if !hasRoleBinding("rhods-dashboard-notebooks", cluster.DefaultNotebooksNamespaceRHOAI) {
		t.Error("missing notebooks RoleBinding")
	}
	if !hasRole("rhods-dashboard-model-registries", testModelRegistryNS) {
		t.Error("missing model-registry Role")
	}
	if !hasRoleBinding("rhods-dashboard-model-registries", testModelRegistryNS) {
		t.Error("missing model-registry RoleBinding")
	}
}

func TestEnsureDashboardNamespacedRBAC_DashboardDisabled(t *testing.T) {
	withTestRegistry(t)
	DefaultRegistry().Add(&dashboardStub{enabled: false})

	rr := newDashboardRBACTestRR(t, cluster.SelfManagedRhoai, nil)

	if err := ensureDashboardNamespacedRBAC(context.Background(), rr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rr.Resources) != 0 {
		t.Fatalf("expected 0 resources when dashboard disabled, got %d", len(rr.Resources))
	}
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

	if len(rr.Resources) != 2 {
		t.Fatalf("expected 2 resources (model-registry only), got %d", len(rr.Resources))
	}
}

func TestEnsureDashboardNamespacedRBAC_ModelRegistryMissing(t *testing.T) {
	enableDashboardInRegistry(t)
	enableWorkbenchesInRegistry(t)

	notebooksNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: cluster.DefaultNotebooksNamespaceRHOAI}}

	rr := newDashboardRBACTestRR(t, cluster.SelfManagedRhoai, nil, notebooksNS)

	if err := ensureDashboardNamespacedRBAC(context.Background(), rr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rr.Resources) != 2 {
		t.Fatalf("expected 2 resources (notebooks only), got %d", len(rr.Resources))
	}
}

func TestEnsureDashboardNamespacedRBAC_WorkbenchesDisabled(t *testing.T) {
	enableDashboardInRegistry(t)
	DefaultRegistry().Add(&workbenchesStub{enabled: false})

	rr := newDashboardRBACTestRR(t, cluster.SelfManagedRhoai, nil)

	if err := ensureDashboardNamespacedRBAC(context.Background(), rr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rr.Resources) != 0 {
		t.Fatalf("expected 0 resources when workbenches disabled, got %d", len(rr.Resources))
	}
}

func TestEnsureDashboardNamespacedRBAC_ODHSAName(t *testing.T) {
	enableDashboardInRegistry(t)
	enableWorkbenchesInRegistry(t)

	notebooksNS := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: cluster.DefaultNotebooksNamespaceODH}}

	rr := newDashboardRBACTestRR(t, cluster.OpenDataHub, nil, notebooksNS)

	if err := ensureDashboardNamespacedRBAC(context.Background(), rr); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rr.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(rr.Resources))
	}

	for _, res := range rr.Resources {
		if res.GetKind() == dashboardRoleKind && res.GetName() != "odh-dashboard-notebooks" {
			t.Errorf("expected ODH SA name in role, got %q", res.GetName())
		}
	}
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

	for _, res := range rr.Resources {
		if res.GetKind() != "RoleBinding" {
			continue
		}

		subjects, ok, _ := unstructured.NestedSlice(res.Object, "subjects")
		if !ok || len(subjects) != 1 {
			t.Fatalf("expected exactly 1 subject in RoleBinding, got %d", len(subjects))
		}

		subj, ok := subjects[0].(map[string]interface{})
		if !ok {
			t.Fatal("subject is not a map")
		}

		if subj["kind"] != string(rbacv1.ServiceAccountKind) {
			t.Errorf("expected ServiceAccount kind, got %v", subj["kind"])
		}
		if subj["name"] != dashboardSANameRHOAI {
			t.Errorf("expected SA name %q, got %v", dashboardSANameRHOAI, subj["name"])
		}
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

	if len(rr.Resources) != 2 {
		t.Fatalf("expected 2 resources (notebooks only), got %d", len(rr.Resources))
	}

	for _, res := range rr.Resources {
		if res.GetNamespace() != customNS {
			t.Errorf("expected namespace %q, got %q", customNS, res.GetNamespace())
		}
	}
}
