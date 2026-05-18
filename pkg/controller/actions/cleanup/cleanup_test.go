package cleanup_test

import (
	"context"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/cleanup"
	ctypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

var testGVK = schema.GroupVersionKind{
	Group:   "test.opendatahub.io",
	Version: "v1",
	Kind:    "TestDependency",
}

var testTarget = cleanup.Target{
	GVK:             testGVK,
	Name:            "cluster",
	FinalizerPrefix: "test-operator.",
}

// invoke runs NewFinalizer as a one-shot call, mirroring the behavior of the
// internal do function without exposing it.
func invoke(ctx context.Context, rr *ctypes.ReconciliationRequest) error {
	return cleanup.NewFinalizer(testTarget)(ctx, rr)
}

func ownerRef(uid types.UID) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: "test.opendatahub.io/v1",
		Kind:       "TestPlatformObject",
		Name:       "test-owner",
		UID:        uid,
	}
}

func makeCR(ownerUID types.UID, finalizers []string) *unstructured.Unstructured {
	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(testGVK)
	cr.SetName("cluster")
	cr.SetOwnerReferences([]metav1.OwnerReference{ownerRef(ownerUID)})
	cr.SetFinalizers(finalizers)
	return cr
}

func TestNewFinalizer_NoCRD(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	instance := &scheme.TestPlatformObject{}
	instance.SetUID("owner-uid")

	rr := &ctypes.ReconciliationRequest{Client: envTest.Client(), Instance: instance}

	g.Expect(invoke(context.Background(), rr)).NotTo(HaveOccurred())
}

func TestNewFinalizer(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()

	_, err = envTest.RegisterCRD(ctx, testGVK, "testdependencies", "testdependency", apiextensionsv1.ClusterScoped)
	g.Expect(err).NotTo(HaveOccurred())

	cli := envTest.Client()
	instance := &scheme.TestPlatformObject{}
	instance.SetUID("owner-uid-1234")

	rr := &ctypes.ReconciliationRequest{Client: cli, Instance: instance}

	cleanupCR := func(t *testing.T) {
		t.Helper()
		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(testGVK)
		if err := cli.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err == nil {
			got.SetFinalizers(nil)
			_ = cli.Update(ctx, got)
			_ = cli.Delete(ctx, got)
		}
	}

	t.Run("no-op when CR does not exist", func(t *testing.T) {
		g := NewWithT(t)
		g.Expect(invoke(ctx, rr)).NotTo(HaveOccurred())
	})

	t.Run("no-op when not owned", func(t *testing.T) {
		g := NewWithT(t)
		cr := makeCR("other-uid", nil)
		g.Expect(cli.Create(ctx, cr)).To(Succeed())
		t.Cleanup(func() { _ = cli.Delete(ctx, cr) })

		g.Expect(invoke(ctx, rr)).NotTo(HaveOccurred())
	})

	t.Run("no-op when terminating with non-matching finalizers only", func(t *testing.T) {
		g := NewWithT(t)
		cr := makeCR(instance.GetUID(), []string{"foregroundDeletion"})
		g.Expect(cli.Create(ctx, cr)).To(Succeed())
		t.Cleanup(func() { cleanupCR(t) })
		g.Expect(cli.Delete(ctx, cr)).To(Succeed())

		g.Expect(invoke(ctx, rr)).NotTo(HaveOccurred())
	})

	t.Run("owned CR lifecycle: delete → wait → gone", func(t *testing.T) {
		g := NewWithT(t)
		cr := makeCR(instance.GetUID(), []string{"test-operator.opendatahub.io/hold"})
		g.Expect(cli.Create(ctx, cr)).To(Succeed())
		t.Cleanup(func() { cleanupCR(t) })

		// Phase 1: CR present → delete triggered, error returned.
		err := invoke(ctx, rr)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("waiting for"))

		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(testGVK)
		g.Expect(cli.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		g.Expect(got.GetDeletionTimestamp()).NotTo(BeNil())

		// Phase 2: still terminating → error.
		g.Expect(invoke(ctx, rr)).To(HaveOccurred())

		// Phase 3: finalizers removed → CR gone → no-op.
		got.SetFinalizers(nil)
		g.Expect(cli.Update(ctx, got)).To(Succeed())
		g.Eventually(func() error {
			return cli.Get(ctx, types.NamespacedName{Name: "cluster"}, got)
		}).Should(MatchError(ContainSubstring("not found")))

		g.Expect(invoke(ctx, rr)).NotTo(HaveOccurred())
	})
}
