package kueue_test

import (
	"context"
	"errors"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	kueuegate "github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade/gates/kueue"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

type kueueGateTestCtx struct {
	cli client.Client
}

func TestKueueGates(t *testing.T) {
	te, err := envt.New()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	t.Cleanup(func() {
		_ = te.Stop()
	})
	if err := installKueueGateCRDs(t.Context(), te); err != nil {
		t.Fatalf("install Kueue gate CRDs: %v", err)
	}

	tc := &kueueGateTestCtx{cli: te.Client()}

	t.Run("clean cluster passes", tc.testCleanClusterPasses)
	t.Run("managed Kueue passes", tc.testManagedKueuePasses)
	t.Run("unmanaged Kueue without operator passes", tc.testUnmanagedKueueWithoutOperatorPasses)
	t.Run("unmanaged Kueue with missing namespace label blocks", tc.testUnmanagedKueueWithMissingNamespaceLabelBlocks)
	t.Run("unmanaged Kueue with managed namespace passes", tc.testUnmanagedKueueWithManagedNamespacePasses)
	t.Run("removed Kueue with queued workloads blocks", tc.testRemovedKueueWithQueuedWorkloadsBlocks)
	t.Run("removed Kueue without queued workloads passes", tc.testRemovedKueueWithoutQueuedWorkloadsPasses)
}

func (tc *kueueGateTestCtx) testCleanClusterPasses(t *testing.T) {
	g := NewWithT(t)

	err := kueuegate.Check(t.Context(), tc.cli, componentApi.KueueComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func (tc *kueueGateTestCtx) testManagedKueuePasses(t *testing.T) {
	g := NewWithT(t)

	obj := renderKueue(t, "Managed")
	g.Expect(tc.cli.Create(t.Context(), obj)).ToNot(HaveOccurred())
	defer deleteObject(g, tc.cli, obj)

	err := kueuegate.Check(t.Context(), tc.cli, componentApi.KueueComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func (tc *kueueGateTestCtx) testUnmanagedKueueWithoutOperatorPasses(t *testing.T) {
	g := NewWithT(t)

	obj := renderKueue(t, "Unmanaged")
	g.Expect(tc.cli.Create(t.Context(), obj)).ToNot(HaveOccurred())
	defer deleteObject(g, tc.cli, obj)

	err := kueuegate.Check(t.Context(), tc.cli, componentApi.KueueComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func (tc *kueueGateTestCtx) testUnmanagedKueueWithMissingNamespaceLabelBlocks(t *testing.T) {
	tc.assertMissingNamespaceLabelBlocks(t, "Unmanaged", "workloads-unmanaged-missing-label")
}

func (tc *kueueGateTestCtx) assertMissingNamespaceLabelBlocks(
	t *testing.T,
	managementState string,
	namespace string,
) {
	t.Helper()

	g := NewWithT(t)

	obj := renderKueue(t, managementState)
	g.Expect(tc.cli.Create(t.Context(), obj)).ToNot(HaveOccurred())
	defer deleteObject(g, tc.cli, obj)

	subscriptionNamespace := namespace + "-operator"
	operatorNamespace := renderNamespace(subscriptionNamespace, nil)
	g.Expect(tc.cli.Create(t.Context(), operatorNamespace)).ToNot(HaveOccurred())

	subscription := renderSubscription(subscriptionNamespace)
	g.Expect(tc.cli.Create(t.Context(), subscription)).ToNot(HaveOccurred())
	defer deleteObject(g, tc.cli, subscription)

	ns := renderNamespace(namespace, nil)
	g.Expect(tc.cli.Create(t.Context(), ns)).ToNot(HaveOccurred())

	notebook := renderNotebook(t, ns.Name)
	g.Expect(tc.cli.Create(t.Context(), notebook)).ToNot(HaveOccurred())
	defer deleteObject(g, tc.cli, notebook)

	err := kueuegate.Check(t.Context(), tc.cli, componentApi.KueueComponentName, "")
	g.Expect(err).To(HaveOccurred())

	var blockingErr *kueuegate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.WorkloadsWithoutKueueNamespaceLabel).To(Equal(1))
}

func (tc *kueueGateTestCtx) testUnmanagedKueueWithManagedNamespacePasses(t *testing.T) {
	g := NewWithT(t)

	obj := renderKueue(t, "Unmanaged")
	g.Expect(tc.cli.Create(t.Context(), obj)).ToNot(HaveOccurred())
	defer deleteObject(g, tc.cli, obj)

	operatorNamespace := renderNamespace("openshift-kueue-operator", nil)
	g.Expect(tc.cli.Create(t.Context(), operatorNamespace)).ToNot(HaveOccurred())

	subscription := renderSubscription(operatorNamespace.Name)
	g.Expect(tc.cli.Create(t.Context(), subscription)).ToNot(HaveOccurred())
	defer deleteObject(g, tc.cli, subscription)

	ns := renderNamespace("workloads-with-label", map[string]string{cluster.KueueManagedLabelKey: "true"})
	g.Expect(tc.cli.Create(t.Context(), ns)).ToNot(HaveOccurred())

	notebook := renderNotebook(t, ns.Name)
	g.Expect(tc.cli.Create(t.Context(), notebook)).ToNot(HaveOccurred())
	defer deleteObject(g, tc.cli, notebook)

	err := kueuegate.Check(t.Context(), tc.cli, componentApi.KueueComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func (tc *kueueGateTestCtx) testRemovedKueueWithQueuedWorkloadsBlocks(t *testing.T) {
	g := NewWithT(t)

	obj := renderKueue(t, "Removed")
	g.Expect(tc.cli.Create(t.Context(), obj)).ToNot(HaveOccurred())
	defer deleteObject(g, tc.cli, obj)

	ns := renderNamespace("workloads-removed-state", nil)
	g.Expect(tc.cli.Create(t.Context(), ns)).ToNot(HaveOccurred())

	notebook := renderNotebook(t, ns.Name)
	g.Expect(tc.cli.Create(t.Context(), notebook)).ToNot(HaveOccurred())
	defer deleteObject(g, tc.cli, notebook)

	err := kueuegate.Check(t.Context(), tc.cli, componentApi.KueueComponentName, "")
	g.Expect(err).To(HaveOccurred())

	var blockingErr *kueuegate.UpgradeBlockedError
	g.Expect(errors.As(err, &blockingErr)).To(BeTrue())
	g.Expect(blockingErr.QueuedWorkloadsWithRemovedKueue).To(Equal(1))
	g.Expect(blockingErr.WorkloadsWithoutKueueNamespaceLabel).To(Equal(0))
}

func (tc *kueueGateTestCtx) testRemovedKueueWithoutQueuedWorkloadsPasses(t *testing.T) {
	g := NewWithT(t)

	obj := renderKueue(t, "Removed")
	g.Expect(tc.cli.Create(t.Context(), obj)).ToNot(HaveOccurred())
	defer deleteObject(g, tc.cli, obj)

	err := kueuegate.Check(t.Context(), tc.cli, componentApi.KueueComponentName, "")
	g.Expect(err).ToNot(HaveOccurred())
}

func installKueueGateCRDs(ctx context.Context, te *envt.EnvT) error {
	if _, err := te.RegisterCRD(
		ctx,
		gvk.Kueue,
		"kueues",
		"kueue",
		apiextensionsv1.ClusterScoped,
		envt.WithPermissiveSchema(),
	); err != nil {
		return err
	}

	if _, err := te.RegisterCRD(
		ctx,
		gvk.Subscription,
		"subscriptions",
		"subscription",
		apiextensionsv1.NamespaceScoped,
		envt.WithPermissiveSchema(),
	); err != nil {
		return err
	}

	for _, resource := range []struct {
		gvk      schema.GroupVersionKind
		plural   string
		singular string
	}{
		{gvk: gvk.Notebook, plural: "notebooks", singular: "notebook"},
		{gvk: gvk.InferenceServices, plural: "inferenceservices", singular: "inferenceservice"},
		{gvk: gvk.LLMInferenceServiceV1Alpha2, plural: "llminferenceservices", singular: "llminferenceservice"},
		{gvk: gvk.RayClusterV1, plural: "rayclusters", singular: "raycluster"},
		{gvk: gvk.RayJobV1, plural: "rayjobs", singular: "rayjob"},
		{gvk: gvk.PyTorchJob, plural: "pytorchjobs", singular: "pytorchjob"},
	} {
		if _, err := te.RegisterCRD(
			ctx,
			resource.gvk,
			resource.plural,
			resource.singular,
			apiextensionsv1.NamespaceScoped,
			envt.WithPermissiveSchema(),
		); err != nil {
			return err
		}
	}

	return nil
}

func deleteObject(g *WithT, cli client.Client, obj client.Object) {
	key := client.ObjectKeyFromObject(obj)
	current, ok := obj.DeepCopyObject().(client.Object)
	g.Expect(ok).To(BeTrue())

	err := cli.Delete(context.Background(), current)
	g.Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())

	g.Eventually(func() error {
		return cli.Get(context.Background(), key, current)
	}).WithTimeout(envt.DefaultMaxWait).WithPolling(envt.DefaultPollInterval).Should(
		MatchError(k8serr.IsNotFound, "IsNotFound"),
	)
}
