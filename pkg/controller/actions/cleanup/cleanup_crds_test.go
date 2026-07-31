package cleanup_test

import (
	"context"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/cleanup"
	ctypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

const (
	crdLabelKey   = "app.opendatahub.io/test-component"
	crdLabelValue = "true"
)

var crdTestGVK = schema.GroupVersionKind{
	Group:   "crdtest.opendatahub.io",
	Version: "v1",
	Kind:    "CRDTestResource",
}

func registerLabeledCRD(
	t *testing.T,
	ctx context.Context,
	et *envt.EnvT,
	gvk schema.GroupVersionKind,
) *apiextensionsv1.CustomResourceDefinition {
	t.Helper()
	g := NewWithT(t)

	crd, err := et.RegisterCRD(ctx, gvk, "crdtestresources", "crdtestresource", apiextensionsv1.ClusterScoped)
	g.Expect(err).NotTo(HaveOccurred())

	g.Expect(et.Client().Get(ctx, client.ObjectKeyFromObject(crd), crd)).To(Succeed())

	crd.Labels = map[string]string{crdLabelKey: crdLabelValue}
	g.Expect(et.Client().Update(ctx, crd)).To(Succeed())

	return crd
}

func makeCRDTestCR(finalizers []string) *unstructured.Unstructured {
	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(crdTestGVK)
	cr.SetGenerateName("test-cr-")
	cr.SetFinalizers(finalizers)
	return cr
}

func invokeCRDCleanup(ctx context.Context, rr *ctypes.ReconciliationRequest) error {
	return cleanup.NewCRDInstanceCleanupFinalizer(crdLabelKey, crdLabelValue)(ctx, rr)
}

func TestFilterOperatorFinalizers(t *testing.T) {
	tests := []struct {
		name            string
		finalizers      []string
		expectedKept    []string
		expectedRemoved []string
	}{
		{
			name:            "all operator-owned finalizers removed",
			finalizers:      []string{"controller.opendatahub.io/cleanup", "reconciler.opendatahub.io/hold"},
			expectedKept:    nil,
			expectedRemoved: []string{"controller.opendatahub.io/cleanup", "reconciler.opendatahub.io/hold"},
		},
		{
			name:            "third-party finalizers preserved",
			finalizers:      []string{"external.io/lock", "controller.opendatahub.io/cleanup"},
			expectedKept:    []string{"external.io/lock"},
			expectedRemoved: []string{"controller.opendatahub.io/cleanup"},
		},
		{
			name:            "no operator-owned finalizers",
			finalizers:      []string{"external.io/lock", "other.io/hold"},
			expectedKept:    []string{"external.io/lock", "other.io/hold"},
			expectedRemoved: nil,
		},
		{
			name:            "empty finalizers",
			finalizers:      []string{},
			expectedKept:    nil,
			expectedRemoved: nil,
		},
		{
			name:            "mixed finalizers preserves order",
			finalizers:      []string{"a.io/first", "test.opendatahub.io/mid", "z.io/last"},
			expectedKept:    []string{"a.io/first", "z.io/last"},
			expectedRemoved: []string{"test.opendatahub.io/mid"},
		},
		{
			name:            "near-match domain in name segment not removed",
			finalizers:      []string{"vendor.io/opendatahub.io-migration-lock"},
			expectedKept:    []string{"vendor.io/opendatahub.io-migration-lock"},
			expectedRemoved: nil,
		},
		{
			name:            "near-match domain prefix not removed",
			finalizers:      []string{"notopendatahub.io/cleanup"},
			expectedKept:    []string{"notopendatahub.io/cleanup"},
			expectedRemoved: nil,
		},
		{
			name:            "exact operator domain finalizer removed",
			finalizers:      []string{"opendatahub.io/cleanup"},
			expectedKept:    nil,
			expectedRemoved: []string{"opendatahub.io/cleanup"},
		},
		{
			name:            "subdomain operator finalizer removed",
			finalizers:      []string{"controller.opendatahub.io/cleanup"},
			expectedKept:    nil,
			expectedRemoved: []string{"controller.opendatahub.io/cleanup"},
		},
		{
			name:            "bare domain without slash",
			finalizers:      []string{"opendatahub.io"},
			expectedKept:    nil,
			expectedRemoved: []string{"opendatahub.io"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			kept, removed := cleanup.FilterOperatorFinalizers(tt.finalizers)
			g.Expect(kept).To(Equal(tt.expectedKept))
			g.Expect(removed).To(Equal(tt.expectedRemoved))
		})
	}
}

func TestCRDInstanceCleanupFinalizer_NoCRDsMatch(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	instance := &scheme.TestPlatformObject{}
	instance.SetUID("owner-uid")
	rr := &ctypes.ReconciliationRequest{Client: envTest.Client(), Instance: instance}

	g.Expect(invokeCRDCleanup(context.Background(), rr)).NotTo(HaveOccurred())
}

func TestCRDInstanceCleanupFinalizer_NoCRInstances(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()
	registerLabeledCRD(t, ctx, envTest, crdTestGVK)

	instance := &scheme.TestPlatformObject{}
	instance.SetUID("owner-uid")
	rr := &ctypes.ReconciliationRequest{Client: envTest.Client(), Instance: instance}

	g.Expect(invokeCRDCleanup(ctx, rr)).NotTo(HaveOccurred())
}

func TestCRDInstanceCleanupFinalizer_OperatorFinalizersRemoved(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()
	cli := envTest.Client()

	registerLabeledCRD(t, ctx, envTest, crdTestGVK)

	cr := makeCRDTestCR([]string{"controller.opendatahub.io/cleanup", "reconciler.opendatahub.io/hold"})
	g.Expect(cli.Create(ctx, cr)).To(Succeed())

	instance := &scheme.TestPlatformObject{}
	instance.SetUID("owner-uid")
	rr := &ctypes.ReconciliationRequest{Client: cli, Instance: instance}

	g.Expect(invokeCRDCleanup(ctx, rr)).NotTo(HaveOccurred())

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(crdTestGVK)
	g.Expect(cli.Get(ctx, types.NamespacedName{Name: cr.GetName()}, got)).To(Succeed())
	g.Expect(got.GetFinalizers()).To(BeEmpty())
}

func TestCRDInstanceCleanupFinalizer_MixedFinalizers(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()
	cli := envTest.Client()

	registerLabeledCRD(t, ctx, envTest, crdTestGVK)

	cr := makeCRDTestCR([]string{"external.io/lock", "controller.opendatahub.io/cleanup"})
	g.Expect(cli.Create(ctx, cr)).To(Succeed())

	instance := &scheme.TestPlatformObject{}
	instance.SetUID("owner-uid")
	rr := &ctypes.ReconciliationRequest{Client: cli, Instance: instance}

	g.Expect(invokeCRDCleanup(ctx, rr)).NotTo(HaveOccurred())

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(crdTestGVK)
	g.Expect(cli.Get(ctx, types.NamespacedName{Name: cr.GetName()}, got)).To(Succeed())
	g.Expect(got.GetFinalizers()).To(Equal([]string{"external.io/lock"}))
}

func TestCRDInstanceCleanupFinalizer_NoOperatorFinalizers(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()
	cli := envTest.Client()

	registerLabeledCRD(t, ctx, envTest, crdTestGVK)

	cr := makeCRDTestCR([]string{"external.io/lock", "other.io/hold"})
	g.Expect(cli.Create(ctx, cr)).To(Succeed())

	instance := &scheme.TestPlatformObject{}
	instance.SetUID("owner-uid")
	rr := &ctypes.ReconciliationRequest{Client: cli, Instance: instance}

	g.Expect(invokeCRDCleanup(ctx, rr)).NotTo(HaveOccurred())

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(crdTestGVK)
	g.Expect(cli.Get(ctx, types.NamespacedName{Name: cr.GetName()}, got)).To(Succeed())
	g.Expect(got.GetFinalizers()).To(Equal([]string{"external.io/lock", "other.io/hold"}))
}

func TestCRDInstanceCleanupFinalizer_MultipleCRs(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()
	cli := envTest.Client()

	registerLabeledCRD(t, ctx, envTest, crdTestGVK)

	cr1 := makeCRDTestCR([]string{"controller.opendatahub.io/cleanup"})
	g.Expect(cli.Create(ctx, cr1)).To(Succeed())

	cr2 := makeCRDTestCR([]string{"reconciler.opendatahub.io/hold", "external.io/lock"})
	g.Expect(cli.Create(ctx, cr2)).To(Succeed())

	cr3 := makeCRDTestCR(nil)
	g.Expect(cli.Create(ctx, cr3)).To(Succeed())

	instance := &scheme.TestPlatformObject{}
	instance.SetUID("owner-uid")
	rr := &ctypes.ReconciliationRequest{Client: cli, Instance: instance}

	g.Expect(invokeCRDCleanup(ctx, rr)).NotTo(HaveOccurred())

	got1 := &unstructured.Unstructured{}
	got1.SetGroupVersionKind(crdTestGVK)
	g.Expect(cli.Get(ctx, types.NamespacedName{Name: cr1.GetName()}, got1)).To(Succeed())
	g.Expect(got1.GetFinalizers()).To(BeEmpty())

	got2 := &unstructured.Unstructured{}
	got2.SetGroupVersionKind(crdTestGVK)
	g.Expect(cli.Get(ctx, types.NamespacedName{Name: cr2.GetName()}, got2)).To(Succeed())
	g.Expect(got2.GetFinalizers()).To(Equal([]string{"external.io/lock"}))

	got3 := &unstructured.Unstructured{}
	got3.SetGroupVersionKind(crdTestGVK)
	g.Expect(cli.Get(ctx, types.NamespacedName{Name: cr3.GetName()}, got3)).To(Succeed())
	g.Expect(got3.GetFinalizers()).To(BeEmpty())
}

func TestCRDInstanceCleanupFinalizer_UnlabeledCRDIgnored(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()
	cli := envTest.Client()

	// Register CRD without the component label
	_, err = envTest.RegisterCRD(ctx, crdTestGVK, "crdtestresources", "crdtestresource", apiextensionsv1.ClusterScoped)
	g.Expect(err).NotTo(HaveOccurred())

	cr := makeCRDTestCR([]string{"controller.opendatahub.io/cleanup"})
	g.Expect(cli.Create(ctx, cr)).To(Succeed())

	instance := &scheme.TestPlatformObject{}
	instance.SetUID("owner-uid")
	rr := &ctypes.ReconciliationRequest{Client: cli, Instance: instance}

	g.Expect(invokeCRDCleanup(ctx, rr)).NotTo(HaveOccurred())

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(crdTestGVK)
	g.Expect(cli.Get(ctx, types.NamespacedName{Name: cr.GetName()}, got)).To(Succeed())
	g.Expect(got.GetFinalizers()).To(Equal([]string{"controller.opendatahub.io/cleanup"}))
}

func TestCRDInstanceCleanupFinalizer_NoOpWhenCRAlreadyDeleted(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()
	cli := envTest.Client()

	registerLabeledCRD(t, ctx, envTest, crdTestGVK)

	cr := makeCRDTestCR([]string{"controller.opendatahub.io/cleanup"})
	g.Expect(cli.Create(ctx, cr)).To(Succeed())

	// Delete the CR (strip finalizers first so it actually goes away)
	cr.SetFinalizers(nil)
	g.Expect(cli.Update(ctx, cr)).To(Succeed())
	g.Expect(cli.Delete(ctx, cr)).To(Succeed())
	g.Eventually(func() error {
		got := &unstructured.Unstructured{}
		got.SetGroupVersionKind(crdTestGVK)
		return cli.Get(ctx, client.ObjectKeyFromObject(cr), got)
	}).Should(HaveOccurred())

	instance := &scheme.TestPlatformObject{}
	instance.SetUID("owner-uid")
	rr := &ctypes.ReconciliationRequest{Client: cli, Instance: instance}

	g.Expect(invokeCRDCleanup(ctx, rr)).NotTo(HaveOccurred())
}

func TestCRDInstanceCleanupFinalizer_NoStorageVersion(t *testing.T) {
	g := NewWithT(t)

	noStorageGVK := schema.GroupVersionKind{
		Group:   "crdtest.opendatahub.io",
		Version: "v1",
		Kind:    "NoStorageResource",
	}

	crd := &apiextensionsv1.CustomResourceDefinition{}
	crd.SetName("nostorageresources.crdtest.opendatahub.io")
	crd.Labels = map[string]string{crdLabelKey: crdLabelValue}
	crd.Spec.Group = "crdtest.opendatahub.io"
	crd.Spec.Names = apiextensionsv1.CustomResourceDefinitionNames{
		Kind:     "NoStorageResource",
		Plural:   "nostorageresources",
		Singular: "nostorageresource",
	}
	crd.Spec.Versions = []apiextensionsv1.CustomResourceDefinitionVersion{
		{Name: "v1", Storage: false, Served: true},
	}

	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(noStorageGVK)
	cr.SetName("test-no-storage-cr")
	cr.SetFinalizers([]string{"controller.opendatahub.io/cleanup"})

	cli, err := fakeclient.New(
		fakeclient.WithObjects(crd, cr),
		fakeclient.WithGVKs(fakeclient.GVKMapping{
			GVK:   noStorageGVK,
			Scope: meta.RESTScopeRoot,
		}),
	)
	g.Expect(err).NotTo(HaveOccurred())

	instance := &scheme.TestPlatformObject{}
	instance.SetUID("owner-uid")
	rr := &ctypes.ReconciliationRequest{Client: cli, Instance: instance}

	g.Expect(invokeCRDCleanup(context.Background(), rr)).NotTo(HaveOccurred())

	got := &unstructured.Unstructured{}
	got.SetGroupVersionKind(noStorageGVK)
	g.Expect(cli.Get(context.Background(), client.ObjectKeyFromObject(cr), got)).To(Succeed())
	g.Expect(got.GetFinalizers()).To(ContainElement("controller.opendatahub.io/cleanup"))
}

func TestCRDInstanceCleanupFinalizer_PatchNotFoundRace(t *testing.T) {
	g := NewWithT(t)

	crd := &apiextensionsv1.CustomResourceDefinition{}
	crd.SetName("crdtestresources.crdtest.opendatahub.io")
	crd.Labels = map[string]string{crdLabelKey: crdLabelValue}
	crd.Spec.Group = crdTestGVK.Group
	crd.Spec.Names = apiextensionsv1.CustomResourceDefinitionNames{
		Kind:     crdTestGVK.Kind,
		Plural:   "crdtestresources",
		Singular: "crdtestresource",
	}
	crd.Spec.Versions = []apiextensionsv1.CustomResourceDefinitionVersion{
		{Name: "v1", Storage: true, Served: true},
	}

	cr := &unstructured.Unstructured{}
	cr.SetGroupVersionKind(crdTestGVK)
	cr.SetName("test-cr-race")
	cr.SetFinalizers([]string{"controller.opendatahub.io/cleanup"})

	cli, err := fakeclient.New(
		fakeclient.WithObjects(crd, cr),
		fakeclient.WithGVKs(fakeclient.GVKMapping{
			GVK:   crdTestGVK,
			Scope: meta.RESTScopeRoot,
		}),
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			Patch: func(
				ctx context.Context,
				c client.WithWatch,
				obj client.Object,
				patch client.Patch,
				opts ...client.PatchOption,
			) error {
				return k8serr.NewNotFound(schema.GroupResource{
					Group:    crdTestGVK.Group,
					Resource: "crdtestresources",
				}, obj.GetName())
			},
		}),
	)
	g.Expect(err).NotTo(HaveOccurred())

	instance := &scheme.TestPlatformObject{}
	instance.SetUID("owner-uid")
	rr := &ctypes.ReconciliationRequest{Client: cli, Instance: instance}

	// Cleanup should succeed — the Patch NotFound error is treated as the CR
	// being deleted between List and Patch, so it is silently skipped.
	g.Expect(invokeCRDCleanup(context.Background(), rr)).NotTo(HaveOccurred())
}
