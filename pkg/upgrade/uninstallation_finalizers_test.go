package upgrade_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	odhgvk "github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func newModuleCR(name string, finalizers ...string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(odhgvk.Dashboard)
	obj.SetName(name)
	if len(finalizers) > 0 {
		obj.SetFinalizers(finalizers)
	}
	return obj
}

func TestRemoveModuleCRFinalizers_StripsExistingFinalizers(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	dashboard := newModuleCR(componentApi.DashboardInstanceName,
		"components.platform.opendatahub.io/cleanup")

	workbenches := &unstructured.Unstructured{}
	workbenches.SetGroupVersionKind(odhgvk.Workbenches)
	workbenches.SetName(componentApi.WorkbenchesInstanceName)
	workbenches.SetFinalizers([]string{"components.platform.opendatahub.io/workbenches-cleanup"})

	mlflow := &unstructured.Unstructured{}
	mlflow.SetGroupVersionKind(odhgvk.MLflowOperator)
	mlflow.SetName(componentApi.MLflowOperatorInstanceName)
	mlflow.SetFinalizers([]string{"mlflow.opendatahub.io/mlflow-operator-protection"})

	cli, err := fakeclient.New(
		fakeclient.WithObjects(dashboard, workbenches, mlflow),
		fakeclient.WithGVKs(
			fakeclient.GVKMapping{GVK: odhgvk.Dashboard, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.Workbenches, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.MLflowOperator, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.FeastOperator, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.Kserve, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.OGX, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.AIGateway, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.MCPLifecycleOperator, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.Trainer, Scope: meta.RESTScopeRoot},
		),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	upgrade.RemoveModuleCRFinalizersForTest(ctx, cli)

	result := &unstructured.Unstructured{}
	result.SetGroupVersionKind(odhgvk.Dashboard)
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: componentApi.DashboardInstanceName}, result)).To(Succeed())
	g.Expect(result.GetFinalizers()).To(BeEmpty(), "dashboard finalizers should be removed")

	result = &unstructured.Unstructured{}
	result.SetGroupVersionKind(odhgvk.Workbenches)
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: componentApi.WorkbenchesInstanceName}, result)).To(Succeed())
	g.Expect(result.GetFinalizers()).To(BeEmpty(), "workbenches finalizers should be removed")

	result = &unstructured.Unstructured{}
	result.SetGroupVersionKind(odhgvk.MLflowOperator)
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: componentApi.MLflowOperatorInstanceName}, result)).To(Succeed())
	g.Expect(result.GetFinalizers()).To(BeEmpty(), "mlflowoperator finalizers should be removed")
}

func TestRemoveModuleCRFinalizers_NoopWithoutFinalizers(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	dashboard := &unstructured.Unstructured{}
	dashboard.SetGroupVersionKind(odhgvk.Dashboard)
	dashboard.SetName(componentApi.DashboardInstanceName)
	dashboard.SetAnnotations(map[string]string{"test": "annotation"})

	cli, err := fakeclient.New(
		fakeclient.WithObjects(dashboard),
		fakeclient.WithGVKs(
			fakeclient.GVKMapping{GVK: odhgvk.Dashboard, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.Workbenches, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.MLflowOperator, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.FeastOperator, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.Kserve, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.OGX, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.AIGateway, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.MCPLifecycleOperator, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.Trainer, Scope: meta.RESTScopeRoot},
		),
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
		fakeclient.WithGVKs(
			fakeclient.GVKMapping{GVK: odhgvk.Dashboard, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.Workbenches, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.MLflowOperator, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.FeastOperator, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.Kserve, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.OGX, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.AIGateway, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.MCPLifecycleOperator, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.Trainer, Scope: meta.RESTScopeRoot},
		),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(func() { upgrade.RemoveModuleCRFinalizersForTest(ctx, cli) }).ShouldNot(Panic(),
		"should handle missing CRs without panicking")
}

func TestRemoveModuleCRFinalizers_CRDeletionTimestamp(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	now := metav1.Now()
	feastOp := &unstructured.Unstructured{}
	feastOp.SetGroupVersionKind(odhgvk.FeastOperator)
	feastOp.SetName(componentApi.FeastOperatorInstanceName)
	feastOp.SetFinalizers([]string{"platform.opendatahub.io/finalizer"})
	feastOp.SetDeletionTimestamp(&now)

	cli, err := fakeclient.New(
		fakeclient.WithObjects(feastOp),
		fakeclient.WithGVKs(
			fakeclient.GVKMapping{GVK: odhgvk.Dashboard, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.Workbenches, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.MLflowOperator, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.FeastOperator, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.Kserve, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.OGX, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.AIGateway, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.MCPLifecycleOperator, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: odhgvk.Trainer, Scope: meta.RESTScopeRoot},
		),
	)
	g.Expect(err).ShouldNot(HaveOccurred())

	upgrade.RemoveModuleCRFinalizersForTest(ctx, cli)

	result := &unstructured.Unstructured{}
	result.SetGroupVersionKind(odhgvk.FeastOperator)
	g.Expect(cli.Get(ctx, client.ObjectKey{Name: componentApi.FeastOperatorInstanceName}, result)).To(Succeed())
	g.Expect(result.GetFinalizers()).To(BeEmpty(),
		"finalizers should be stripped even from CRs with deletionTimestamp")
}
