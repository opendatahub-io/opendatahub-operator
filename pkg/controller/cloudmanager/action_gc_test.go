//nolint:testpackage // white-box tests for unexported GC predicate
package cloudmanager

import (
	"context"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/cleanup"
	odhTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhAnnotations "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

const (
	testUID        = k8stypes.UID("test-uid-123")
	testGeneration = int64(5)
)

// newTestRR creates a minimal ReconciliationRequest for unit tests.
func newTestRR(resourcesList []unstructured.Unstructured) *odhTypes.ReconciliationRequest {
	instance := &ccmv1alpha1.AzureKubernetesEngine{
		ObjectMeta: metav1.ObjectMeta{
			UID:        testUID,
			Generation: testGeneration,
		},
	}
	return &odhTypes.ReconciliationRequest{
		Instance:  instance,
		Resources: resourcesList,
	}
}

// newObj creates an unstructured object with the given GVK, name, namespace, and annotations.
func newObj(objGVK schema.GroupVersionKind, name, namespace string, anns map[string]string) unstructured.Unstructured {
	obj := unstructured.Unstructured{}
	obj.SetGroupVersionKind(objGVK)
	obj.SetName(name)
	obj.SetNamespace(namespace)
	obj.SetAnnotations(anns)
	return obj
}

// simpleObj creates an unstructured object for annotation-only tests.
func simpleObj(anns map[string]string) unstructured.Unstructured {
	return newObj(schema.GroupVersionKind{}, "", "", anns)
}

// ccmAnns returns the standard cloud manager annotations for the test UID and generation.
func ccmAnns(uid string, generation string) map[string]string {
	return map[string]string{
		labels.ODHInfrastructurePrefix + odhAnnotations.SuffixInstanceUID:        uid,
		labels.ODHInfrastructurePrefix + odhAnnotations.SuffixInstanceGeneration: generation,
	}
}

// legacyCCMAnns returns the old platform.opendatahub.io annotations for the test UID and generation.
func legacyCCMAnns(uid string, generation string) map[string]string {
	return map[string]string{
		labels.ODHPlatformPrefix + odhAnnotations.SuffixInstanceUID:        uid,
		labels.ODHPlatformPrefix + odhAnnotations.SuffixInstanceGeneration: generation,
	}
}

var testProtectedObjects = []ProtectedObject{
	{Group: "cert-manager.io", Kind: "ClusterIssuer", Name: "opendatahub-selfsigned-issuer"},
	{Group: "cert-manager.io", Kind: "Certificate", Name: "opendatahub-ca", Namespace: "cert-manager"},
}

func TestNewGCPredicate(t *testing.T) {
	t.Parallel()

	rr := newTestRR(nil)

	cases := []struct {
		name       string
		obj        unstructured.Unstructured
		wantDelete bool
	}{
		{
			name:       "missing both annotations — keep",
			obj:        simpleObj(nil),
			wantDelete: false,
		},
		{
			name:       "missing UID annotation only — keep",
			obj:        simpleObj(map[string]string{labels.ODHInfrastructurePrefix + odhAnnotations.SuffixInstanceGeneration: "5"}),
			wantDelete: false,
		},
		{
			name:       "missing generation annotation only — keep",
			obj:        simpleObj(map[string]string{labels.ODHInfrastructurePrefix + odhAnnotations.SuffixInstanceUID: string(testUID)}),
			wantDelete: false,
		},
		{
			name:       "empty-string UID annotation — keep",
			obj:        simpleObj(ccmAnns("", "5")),
			wantDelete: false,
		},
		{
			name: "empty-string generation annotation — keep",
			obj: simpleObj(map[string]string{
				labels.ODHInfrastructurePrefix + odhAnnotations.SuffixInstanceUID:        string(testUID),
				labels.ODHInfrastructurePrefix + odhAnnotations.SuffixInstanceGeneration: "",
			}),
			wantDelete: false,
		},
		{
			name: "protected object — keep, even with UID mismatch",
			obj: newObj(
				schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "ClusterIssuer"},
				"opendatahub-selfsigned-issuer", "",
				ccmAnns("different-uid", "5"),
			),
			wantDelete: false,
		},
		{
			name: "protected object with different API version — keep (version-agnostic)",
			obj: newObj(
				schema.GroupVersionKind{Group: "cert-manager.io", Version: "v2beta1", Kind: "ClusterIssuer"},
				"opendatahub-selfsigned-issuer", "",
				ccmAnns(string(testUID), "3"),
			),
			wantDelete: false,
		},
		{
			name: "protected namespaced object — keep",
			obj: newObj(
				schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "Certificate"},
				"opendatahub-ca", "cert-manager",
				ccmAnns(string(testUID), "3"),
			),
			wantDelete: false,
		},
		{
			name: "same GVK+name as protected but different namespace — not protected, delete",
			obj: newObj(
				schema.GroupVersionKind{Group: "cert-manager.io", Version: "v1", Kind: "Certificate"},
				"opendatahub-ca", "other-namespace",
				ccmAnns(string(testUID), "3"),
			),
			wantDelete: true,
		},
		{
			name:       "UID mismatch — orphaned resource, delete",
			obj:        simpleObj(ccmAnns("different-uid", "5")),
			wantDelete: true,
		},
		{
			name:       "UID matches, generation matches — keep",
			obj:        simpleObj(ccmAnns(string(testUID), "5")),
			wantDelete: false,
		},
		{
			name:       "UID matches, generation mismatch — delete",
			obj:        simpleObj(ccmAnns(string(testUID), "3")),
			wantDelete: true,
		},
		{
			name:       "malformed InstanceGeneration — skip (do not delete)",
			obj:        simpleObj(ccmAnns(string(testUID), "not-a-number")),
			wantDelete: false,
		},
		// Legacy platform.opendatahub.io annotation fallback cases.
		{
			name:       "legacy annotations: UID matches, generation matches — keep",
			obj:        simpleObj(legacyCCMAnns(string(testUID), "5")),
			wantDelete: false,
		},
		{
			name:       "legacy annotations: UID mismatch — delete",
			obj:        simpleObj(legacyCCMAnns("different-uid", "5")),
			wantDelete: true,
		},
		{
			name:       "legacy annotations: UID matches, generation mismatch — delete",
			obj:        simpleObj(legacyCCMAnns(string(testUID), "3")),
			wantDelete: true,
		},
		{
			name:       "legacy annotations: malformed generation — skip (do not delete)",
			obj:        simpleObj(legacyCCMAnns(string(testUID), "not-a-number")),
			wantDelete: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			pred := newGCPredicate(testProtectedObjects)
			got, err := pred(rr, tc.obj)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got).To(Equal(tc.wantDelete))
		})
	}
}

func TestNewGCPredicate_NoProtectedObjects(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	rr := newTestRR(nil)
	pred := newGCPredicate(nil)

	// Without protected objects, generation mismatch deletes.
	obj := simpleObj(ccmAnns(string(testUID), "3"))
	got, err := pred(rr, obj)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(got).To(BeTrue())
}

func TestNewGCAction(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		resourceID       string
		operatorNS       string
		protectedObjects []ProtectedObject
		wantErrContains  string
	}{
		{
			name:            "empty resourceID returns error",
			resourceID:      "",
			operatorNS:      "test-ns",
			wantErrContains: "resourceID is required",
		},
		{
			name:            "empty operatorNamespace returns error",
			resourceID:      "test-resource",
			operatorNS:      "",
			wantErrContains: "operatorNamespace is required",
		},
		{
			name:       "protected object with empty Kind returns error",
			resourceID: "test-resource",
			operatorNS: "test-ns",
			protectedObjects: []ProtectedObject{
				{Group: "cert-manager.io", Name: "test"},
			},
			wantErrContains: "requires both Kind and Name",
		},
		{
			name:       "protected object with empty Name returns error",
			resourceID: "test-resource",
			operatorNS: "test-ns",
			protectedObjects: []ProtectedObject{
				{Group: "cert-manager.io", Kind: "Certificate"},
			},
			wantErrContains: "requires both Kind and Name",
		},
		{
			name:       "valid parameters return non-nil action",
			resourceID: "test-resource",
			operatorNS: "test-ns",
			protectedObjects: []ProtectedObject{
				{Group: "cert-manager.io", Kind: "Certificate", Name: "test-cert"},
			},
		},
		{
			name:             "nil protected objects is valid",
			resourceID:       "test-resource",
			operatorNS:       "test-ns",
			protectedObjects: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			fn, err := NewGCAction(tc.resourceID, tc.operatorNS, tc.protectedObjects, nil)
			if tc.wantErrContains != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tc.wantErrContains))
				g.Expect(fn).To(BeNil())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(fn).NotTo(BeNil())
		})
	}
}

// TestNewGCAction_CleanupTargets verifies that the GC action runs the cleanup
// pre-phase for stale dependency CRs before proceeding with the normal GC scan.
func TestNewGCAction_CleanupTargets(t *testing.T) {
	g := NewWithT(t)

	envTest, err := envt.New()
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = envTest.Stop() })

	ctx := context.Background()

	gcTargetGVK := schema.GroupVersionKind{
		Group:   "test.opendatahub.io",
		Version: "v1",
		Kind:    "TestDependency",
	}

	_, err = envTest.RegisterCRD(ctx,
		gcTargetGVK,
		"testdependencies", "testdependency",
		apiextensionsv1.ClusterScoped,
	)
	g.Expect(err).NotTo(HaveOccurred())

	cli := envTest.Client()

	instance := &scheme.TestPlatformObject{}
	instance.SetUID("owner-uid-1234")
	instance.SetGeneration(5)

	gcTarget := cleanup.Target{
		GVK:             gcTargetGVK,
		Name:            "cluster",
		FinalizerPrefix: "test-operator.",
	}

	makeRR := func() *odhTypes.ReconciliationRequest {
		return &odhTypes.ReconciliationRequest{
			Client:   cli,
			Instance: instance,
		}
	}

	makeCR := func(ownerUID k8stypes.UID, genAnnotation string, finalizers []string) *unstructured.Unstructured {
		cr := &unstructured.Unstructured{}
		cr.SetGroupVersionKind(gcTargetGVK)
		cr.SetName("cluster")
		cr.SetOwnerReferences([]metav1.OwnerReference{{
			APIVersion: "test.opendatahub.io/v1",
			Kind:       "TestPlatformObject",
			Name:       "test-owner",
			UID:        ownerUID,
		}})
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
		got.SetGroupVersionKind(gcTargetGVK)
		if err := cli.Get(ctx, k8stypes.NamespacedName{Name: "cluster"}, got); err == nil {
			got.SetFinalizers(nil)
			_ = cli.Update(ctx, got)
			_ = cli.Delete(ctx, got)
		}
	}

	action, err := NewGCAction("test-resource", "test-ns", nil, []cleanup.Target{gcTarget})
	g.Expect(err).NotTo(HaveOccurred())

	t.Run("stale target with pending finalizers — pre-phase blocks GC", func(t *testing.T) {
		g := NewWithT(t)
		t.Cleanup(func() { cleanupCR(t) })

		cr := makeCR(instance.GetUID(), "3", []string{"test-operator.opendatahub.io/hold"})
		g.Expect(cli.Create(ctx, cr)).To(Succeed())

		err := action(ctx, makeRR())
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("waiting for"))
	})

	t.Run("stale target not owned — pre-phase is no-op", func(t *testing.T) {
		g := NewWithT(t)
		t.Cleanup(func() { cleanupCR(t) })

		cr := makeCR("other-uid", "3", []string{"test-operator.opendatahub.io/hold"})
		g.Expect(cli.Create(ctx, cr)).To(Succeed())

		err := action(ctx, makeRR())
		if err != nil {
			g.Expect(err.Error()).NotTo(ContainSubstring("waiting for"))
		}
	})
}
