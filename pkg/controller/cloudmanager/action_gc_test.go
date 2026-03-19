//nolint:testpackage // white-box tests for unexported GC predicate, dep-set cache, and namespace resolver
package cloudmanager

import (
	"context"
	"os"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8stypes "k8s.io/apimachinery/pkg/types"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"
	odhTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"

	. "github.com/onsi/gomega"
)

const (
	testUID        = k8stypes.UID("test-uid-123")
	testGeneration = int64(5)
)

// newTestRR creates a minimal ReconciliationRequest for unit tests.
// Only Instance and Resources are set; other fields are nil.
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

// newObj creates an unstructured object with the given labels and annotations.
func newObj(lbls, anns map[string]string) unstructured.Unstructured {
	obj := unstructured.Unstructured{}
	obj.SetLabels(lbls)
	obj.SetAnnotations(anns)
	return obj
}

// ccmAnns returns the standard cloud manager annotations for the test UID and generation.
func ccmAnns(uid string, generation string) map[string]string {
	return map[string]string{
		annotations.InstanceUID:        uid,
		annotations.InstanceGeneration: generation,
	}
}

func TestCurrentManagedDeps(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		resources []unstructured.Unstructured
		want      map[string]struct{}
	}{
		{
			name:      "empty resources yields empty map",
			resources: nil,
			want:      map[string]struct{}{},
		},
		{
			name: "resources without dep label are excluded",
			resources: []unstructured.Unstructured{
				newObj(nil, nil),
				newObj(map[string]string{"other": "label"}, nil),
			},
			want: map[string]struct{}{},
		},
		{
			name: "resources with dep label are included",
			resources: []unstructured.Unstructured{
				newObj(map[string]string{labels.InfrastructureDependency: "chart-a"}, nil),
				newObj(map[string]string{labels.InfrastructureDependency: "chart-b"}, nil),
			},
			want: map[string]struct{}{"chart-a": {}, "chart-b": {}},
		},
		{
			name: "duplicate dep labels are deduplicated",
			resources: []unstructured.Unstructured{
				newObj(map[string]string{labels.InfrastructureDependency: "chart-a"}, nil),
				newObj(map[string]string{labels.InfrastructureDependency: "chart-a"}, nil),
			},
			want: map[string]struct{}{"chart-a": {}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			rr := newTestRR(tc.resources)
			got := currentManagedDeps(rr)
			g.Expect(got).To(Equal(tc.want))
		})
	}
}

func TestNewGCPredicate(t *testing.T) {
	t.Parallel()

	depResources := []unstructured.Unstructured{
		newObj(map[string]string{labels.InfrastructureDependency: "managed-chart"}, nil),
	}
	rr := newTestRR(depResources)

	cases := []struct {
		name            string
		obj             unstructured.Unstructured
		wantDelete      bool
		wantErrContains string
	}{
		{
			name:       "missing both annotations — keep",
			obj:        newObj(nil, nil),
			wantDelete: false,
		},
		{
			name:       "missing UID annotation only — keep",
			obj:        newObj(nil, map[string]string{annotations.InstanceGeneration: "5"}),
			wantDelete: false,
		},
		{
			name:       "missing generation annotation only — keep",
			obj:        newObj(nil, map[string]string{annotations.InstanceUID: string(testUID)}),
			wantDelete: false,
		},
		{
			name: "retain label — always keep, even with UID mismatch",
			obj: newObj(
				map[string]string{labels.InfrastructureGCPolicy: labels.GCPolicyRetain},
				ccmAnns("different-uid", "5"),
			),
			wantDelete: false,
		},
		{
			name:       "UID mismatch — orphaned resource, delete",
			obj:        newObj(nil, ccmAnns("different-uid", "5")),
			wantDelete: true,
		},
		{
			name: "dep label not in managed deps — Unmanaged, keep",
			obj: newObj(
				map[string]string{labels.InfrastructureDependency: "unmanaged-chart"},
				ccmAnns(string(testUID), "5"),
			),
			wantDelete: false,
		},
		{
			name: "dep label in managed deps, generation matches — keep",
			obj: newObj(
				map[string]string{labels.InfrastructureDependency: "managed-chart"},
				ccmAnns(string(testUID), "5"),
			),
			wantDelete: false,
		},
		{
			name: "dep label in managed deps, generation mismatch — delete",
			obj: newObj(
				map[string]string{labels.InfrastructureDependency: "managed-chart"},
				ccmAnns(string(testUID), "3"),
			),
			wantDelete: true,
		},
		{
			name:       "no dep label, generation matches — keep",
			obj:        newObj(nil, ccmAnns(string(testUID), "5")),
			wantDelete: false,
		},
		{
			name:       "no dep label, generation mismatch — delete",
			obj:        newObj(nil, ccmAnns(string(testUID), "3")),
			wantDelete: true,
		},
		{
			name:            "malformed InstanceGeneration — error",
			obj:             newObj(nil, ccmAnns(string(testUID), "not-a-number")),
			wantErrContains: "cannot parse InstanceGeneration",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			pred := newGCPredicate()
			got, err := pred(rr, tc.obj)
			if tc.wantErrContains != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err.Error()).To(ContainSubstring(tc.wantErrContains))
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(got).To(Equal(tc.wantDelete))
		})
	}
}

func TestNewGCPredicate_CachesDepMapPerReconcile(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	rr1 := newTestRR([]unstructured.Unstructured{
		newObj(map[string]string{labels.InfrastructureDependency: "chart-a"}, nil),
	})
	rr2 := newTestRR([]unstructured.Unstructured{
		newObj(map[string]string{labels.InfrastructureDependency: "chart-b"}, nil),
	})

	obj := newObj(
		map[string]string{labels.InfrastructureDependency: "chart-a"},
		ccmAnns(string(testUID), "3"),
	)

	pred := newGCPredicate()

	// rr1 has chart-a as managed → generation mismatch → delete.
	delete1, err := pred(rr1, obj)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(delete1).To(BeTrue())

	// rr2 does NOT have chart-a as managed → Unmanaged → keep.
	delete2, err := pred(rr2, obj)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(delete2).To(BeFalse())
}

func TestOperatorNamespaceFn_EnvVarFallback(t *testing.T) {
	g := NewWithT(t)

	t.Setenv("OPERATOR_NAMESPACE", "test-operator-ns")

	ns, err := operatorNamespaceFn(context.Background(), &odhTypes.ReconciliationRequest{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(ns).To(Equal("test-operator-ns"))
}

func TestOperatorNamespaceFn_NeitherAvailable(t *testing.T) {
	g := NewWithT(t)

	// Ensure env var is unset for this test.
	t.Setenv("OPERATOR_NAMESPACE", "")

	_, err := operatorNamespaceFn(context.Background(), &odhTypes.ReconciliationRequest{})
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("operator namespace unavailable"))
}

// Compile-time check: ensure resources package is used (for GetLabel / GetAnnotation
// indirectly via newObj helper). This avoids accidental removal of the import.
var _ = resources.GetLabel
var _ = os.Getenv
