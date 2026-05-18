package cloudmanager

import (
	"context"
	"strconv"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/cleanup"
	ctypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhAnnotations "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
	"github.com/rs/xid"

	. "github.com/onsi/gomega"
)

var staleTestGVK = schema.GroupVersionKind{
	Group:   "test.opendatahub.io",
	Version: "v1",
	Kind:    "TestDependencyCR",
}

var staleTestTarget = cleanup.Target{
	GVK:             staleTestGVK,
	Name:            "cluster",
	FinalizerPrefix: "test-operator.",
}

func TestCleanupStaleCR(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()

	_, err = envTest.RegisterCRD(ctx,
		staleTestGVK,
		"testdependencycrs", "testdependencycr",
		apiextensionsv1.ClusterScoped,
	)
	g.Expect(err).NotTo(HaveOccurred())

	cli := envTest.Client()
	instance := &scheme.TestPlatformObject{}
	ownerUID := xid.New().String()
	instance.SetUID(types.UID(ownerUID))
	instance.SetGeneration(5)

	rr := &ctypes.ReconciliationRequest{
		Client:   cli,
		Instance: instance,
	}

	makeOwnerRef := func(uid types.UID) metav1.OwnerReference {
		return metav1.OwnerReference{
			APIVersion: "test.opendatahub.io/v1",
			Kind:       "TestPlatformObject",
			Name:       "test-owner",
			UID:        uid,
		}
	}

	makeCR := func(ownerUID types.UID, genAnnotation string, finalizers []string) *unstructured.Unstructured {
		cr := &unstructured.Unstructured{}
		cr.SetGroupVersionKind(staleTestGVK)
		cr.SetName("cluster")
		cr.SetOwnerReferences([]metav1.OwnerReference{makeOwnerRef(ownerUID)})
		if genAnnotation != "" {
			cr.SetAnnotations(map[string]string{
				labels.ODHInfrastructurePrefix + odhAnnotations.SuffixInstanceGeneration: genAnnotation,
			})
		}
		cr.SetFinalizers(finalizers)
		return cr
	}

	cleanupCR := func(t *testing.T) {
		t.Helper()
		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(staleTestGVK)
		if err := cli.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err == nil {
			got.SetFinalizers(nil)
			_ = cli.Update(ctx, got)
			_ = cli.Delete(ctx, got)
		}
	}

	t.Run("no-op when CR does not exist", func(t *testing.T) {
		g := NewWithT(t)

		err := cleanupStaleCR(ctx, rr, staleTestTarget)
		g.Expect(err).NotTo(HaveOccurred())
	})

	t.Run("no-op when generation matches current", func(t *testing.T) {
		g := NewWithT(t)
		t.Cleanup(func() { cleanupCR(t) })

		cr := makeCR(instance.GetUID(), strconv.FormatInt(instance.GetGeneration(), 10), nil)
		g.Expect(cli.Create(ctx, cr)).To(Succeed())

		err := cleanupStaleCR(ctx, rr, staleTestTarget)
		g.Expect(err).NotTo(HaveOccurred())

		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(staleTestGVK)
		g.Expect(cli.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		g.Expect(got.GetDeletionTimestamp()).To(BeNil())
	})

	t.Run("triggers cleanup when generation is stale", func(t *testing.T) {
		g := NewWithT(t)
		t.Cleanup(func() { cleanupCR(t) })

		cr := makeCR(instance.GetUID(), "3", []string{"test-operator.opendatahub.io/hold"})
		g.Expect(cli.Create(ctx, cr)).To(Succeed())

		err := cleanupStaleCR(ctx, rr, staleTestTarget)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("waiting for"))

		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(staleTestGVK)
		g.Expect(cli.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		g.Expect(got.GetDeletionTimestamp()).NotTo(BeNil())
	})
}
