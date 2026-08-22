package upgrade_test

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	odhgvk "github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

var allModuleGVKMappings = []fakeclient.GVKMapping{
	{GVK: odhgvk.Dashboard, Scope: meta.RESTScopeRoot},
	{GVK: odhgvk.Workbenches, Scope: meta.RESTScopeRoot},
	{GVK: odhgvk.MLflowOperator, Scope: meta.RESTScopeRoot},
	{GVK: odhgvk.FeastOperator, Scope: meta.RESTScopeRoot},
	{GVK: odhgvk.Kserve, Scope: meta.RESTScopeRoot},
	{GVK: odhgvk.OGX, Scope: meta.RESTScopeRoot},
	{GVK: odhgvk.AIGateway, Scope: meta.RESTScopeRoot},
	{GVK: odhgvk.MCPLifecycleOperator, Scope: meta.RESTScopeRoot},
	{GVK: odhgvk.Trainer, Scope: meta.RESTScopeRoot},
}

func newUnstructuredCR(gvk schema.GroupVersionKind, name string, finalizers ...string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	obj.SetName(name)
	if len(finalizers) > 0 {
		obj.SetFinalizers(finalizers)
	}
	return obj
}

func TestRemoveModuleCRFinalizers_StripsExistingFinalizers(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	dashboard := newUnstructuredCR(odhgvk.Dashboard, componentApi.DashboardInstanceName,
		"components.platform.opendatahub.io/cleanup")

	workbenches := newUnstructuredCR(odhgvk.Workbenches, componentApi.WorkbenchesInstanceName,
		"components.platform.opendatahub.io/workbenches-cleanup")

	mlflow := newUnstructuredCR(odhgvk.MLflowOperator, componentApi.MLflowOperatorInstanceName,
		"mlflow.opendatahub.io/mlflow-operator-protection")

	cli, err := fakeclient.New(
		fakeclient.WithObjects(dashboard, workbenches, mlflow),
		fakeclient.WithGVKs(allModuleGVKMappings...),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	upgrade.RemoveModuleCRFinalizersForTest(ctx, cli)

	for _, tc := range []struct {
		gvk  schema.GroupVersionKind
		name string
	}{
		{odhgvk.Dashboard, componentApi.DashboardInstanceName},
		{odhgvk.Workbenches, componentApi.WorkbenchesInstanceName},
		{odhgvk.MLflowOperator, componentApi.MLflowOperatorInstanceName},
	} {
		result := &unstructured.Unstructured{}
		result.SetGroupVersionKind(tc.gvk)
		g.Expect(cli.Get(ctx, client.ObjectKey{Name: tc.name}, result)).To(Succeed())
		g.Expect(result.GetFinalizers()).To(BeEmpty(),
			"%s finalizers should be removed", tc.gvk.Kind)
	}
}

func TestRemoveModuleCRFinalizers_NoopWithoutFinalizers(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	dashboard := newUnstructuredCR(odhgvk.Dashboard, componentApi.DashboardInstanceName)
	dashboard.SetAnnotations(map[string]string{"test": "annotation"})

	cli, err := fakeclient.New(
		fakeclient.WithObjects(dashboard),
		fakeclient.WithGVKs(allModuleGVKMappings...),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	upgrade.RemoveModuleCRFinalizersForTest(ctx, cli)

	result := &unstructured.Unstructured{}
	result.SetGroupVersionKind(odhgvk.Dashboard)
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: componentApi.DashboardInstanceName}, result)).To(Succeed())
	g.Expect(result.GetAnnotations()).To(HaveKeyWithValue("test", "annotation"),
		"CR should not be modified when no finalizers present")
}

func TestRemoveModuleCRFinalizers_HandlesAbsentCRs(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cli, err := fakeclient.New(
		fakeclient.WithGVKs(allModuleGVKMappings...),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(func() { upgrade.RemoveModuleCRFinalizersForTest(ctx, cli) }).ShouldNot(Panic(),
		"should handle missing CRs without panicking")
}

func TestRemoveModuleCRFinalizers_PatchErrorDoesNotPanic(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	feast := newUnstructuredCR(odhgvk.FeastOperator, componentApi.FeastOperatorInstanceName,
		"platform.opendatahub.io/finalizer")

	cli, err := fakeclient.New(
		fakeclient.WithObjects(feast),
		fakeclient.WithGVKs(allModuleGVKMappings...),
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
				return context.DeadlineExceeded
			},
		}),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(func() { upgrade.RemoveModuleCRFinalizersForTest(ctx, cli) }).ShouldNot(Panic(),
		"should handle patch errors without panicking")
}
