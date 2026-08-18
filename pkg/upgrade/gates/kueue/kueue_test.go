package kueue_test

import (
	"embed"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	kueuegate "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/kueue"
	tp "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

//go:embed resources
var resourcesFS embed.FS

func TestRegister_CleanClusterPasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cli, err := newKueueClient()
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.KueueComponentName, kueuegate.Check)

	err = provision.GetUpgradeCheck(componentApi.KueueComponentName)(ctx, cli, componentApi.KueueComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func TestRegister_ManagedKueueBlocks(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cli, err := newKueueClient(
		renderKueue(t, "Managed"),
	)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.KueueComponentName, kueuegate.Check)

	err = provision.GetUpgradeCheck(componentApi.KueueComponentName)(ctx, cli, componentApi.KueueComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func TestRegister_UnmanagedKueueWithoutOperatorPasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cli, err := newKueueClient(
		renderKueue(t, "Unmanaged"),
	)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.KueueComponentName, kueuegate.Check)

	err = provision.GetUpgradeCheck(componentApi.KueueComponentName)(ctx, cli, componentApi.KueueComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func TestRegister_UnmanagedKueueWithMissingNamespaceLabelBlocks(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cli, err := newKueueClient(
		renderKueue(t, "Unmanaged"),
		renderSubscription("openshift-kueue-operator"),
		renderNamespace("workloads", nil),
		renderNotebook(t, "workloads"),
	)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.KueueComponentName, kueuegate.Check)

	err = provision.GetUpgradeCheck(componentApi.KueueComponentName)(ctx, cli, componentApi.KueueComponentName, "")
	g.Expect(err).To(HaveOccurred())

	var blockingErr *kueuegate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.WorkloadsWithoutKueueNamespaceLabel).To(Equal(1))
}

func TestRegister_UnmanagedKueueWithManagedNamespacePasses(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cli, err := newKueueClient(
		renderKueue(t, "Unmanaged"),
		renderSubscription("openshift-kueue-operator"),
		renderNamespace("workloads", map[string]string{cluster.KueueManagedLabelKey: "true"}),
		renderNotebook(t, "workloads"),
	)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.KueueComponentName, kueuegate.Check)

	err = provision.GetUpgradeCheck(componentApi.KueueComponentName)(ctx, cli, componentApi.KueueComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func TestRegister_RemovedKueueIgnoresLabeledWorkloads(t *testing.T) {
	g := NewWithT(t)
	ctx := t.Context()

	cli, err := newKueueClient(
		renderKueue(t, "Removed"),
		renderNamespace("workloads", nil),
		renderNotebook(t, "workloads"),
	)
	g.Expect(err).ToNot(HaveOccurred())

	provision.RegisterUpgradeCheck(componentApi.KueueComponentName, kueuegate.Check)

	err = provision.GetUpgradeCheck(componentApi.KueueComponentName)(ctx, cli, componentApi.KueueComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func newKueueClient(objects ...client.Object) (client.Client, error) {
	return fakeclient.New(
		fakeclient.WithObjects(objects...),
		fakeclient.WithGVKs(
			fakeclient.GVKMapping{GVK: gvk.Kueue, Scope: meta.RESTScopeRoot},
			fakeclient.GVKMapping{GVK: gvk.Notebook, Scope: meta.RESTScopeNamespace},
			fakeclient.GVKMapping{GVK: gvk.Subscription, Scope: meta.RESTScopeNamespace},
		),
	)
}

func renderKueue(t *testing.T, managementState string) *unstructured.Unstructured {
	t.Helper()

	g := NewWithT(t)
	obj, err := tp.RenderObject(resourcesFS, "resources/kueue.tmpl.yaml", map[string]any{
		"ManagementState": managementState,
	})
	g.Expect(err).ToNot(HaveOccurred())

	return obj
}

func renderNotebook(t *testing.T, namespace string) *unstructured.Unstructured {
	t.Helper()

	g := NewWithT(t)
	obj, err := tp.RenderObject(resourcesFS, "resources/notebook.tmpl.yaml", map[string]any{
		"Name":      "labeled-notebook",
		"Namespace": namespace,
		"QueueName": "test-queue",
	})
	g.Expect(err).ToNot(HaveOccurred())

	return obj
}

func renderNamespace(name string, labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func renderSubscription(namespace string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk.Subscription)
	obj.SetName("kueue-operator")
	obj.SetNamespace(namespace)

	return obj
}
