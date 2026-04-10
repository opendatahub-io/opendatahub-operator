//nolint:testpackage // white-box tests for unexported GC predicate
package cloudmanager

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"
	odhTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"

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
		annotations.InstanceUID:        uid,
		annotations.InstanceGeneration: generation,
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
			obj:        simpleObj(map[string]string{annotations.InstanceGeneration: "5"}),
			wantDelete: false,
		},
		{
			name:       "missing generation annotation only — keep",
			obj:        simpleObj(map[string]string{annotations.InstanceUID: string(testUID)}),
			wantDelete: false,
		},
		{
			name:       "empty-string UID annotation — keep",
			obj:        simpleObj(ccmAnns("", "5")),
			wantDelete: false,
		},
		{
			name:       "empty-string generation annotation — keep",
			obj:        simpleObj(map[string]string{annotations.InstanceUID: string(testUID), annotations.InstanceGeneration: ""}),
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
			fn, err := NewGCAction(tc.resourceID, tc.operatorNS, tc.protectedObjects)
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
