//nolint:testpackage
package deploy

import (
	"testing"

	"github.com/onsi/gomega/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"

	. "github.com/onsi/gomega"
)

func TestIsLegacyOwnerRef(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	tests := []struct {
		name     string
		ownerRef metav1.OwnerReference
		matcher  types.GomegaMatcher
	}{
		{
			name: "Valid DataScienceCluster owner reference",
			ownerRef: metav1.OwnerReference{
				APIVersion: gvk.DataScienceCluster.GroupVersion().String(),
				Kind:       gvk.DataScienceCluster.Kind,
			},
			matcher: BeTrue(),
		},
		{
			name: "Valid DSCInitialization owner reference",
			ownerRef: metav1.OwnerReference{
				APIVersion: gvk.DSCInitialization.GroupVersion().String(),
				Kind:       gvk.DSCInitialization.Kind,
			},
			matcher: BeTrue(),
		},
		{
			name: "Invalid owner reference (different group)",
			ownerRef: metav1.OwnerReference{
				APIVersion: "othergroup/v1",
				Kind:       gvk.DSCInitialization.Kind,
			},
			matcher: BeFalse(),
		},
		{
			name: "Invalid owner reference (different kind)",
			ownerRef: metav1.OwnerReference{
				APIVersion: gvk.DSCInitialization.GroupVersion().String(),
				Kind:       "OtherKind",
			},
			matcher: BeFalse(),
		},
		{
			name: "Invalid owner reference (different group and kind)",
			ownerRef: metav1.OwnerReference{
				APIVersion: "othergroup/v1",
				Kind:       "OtherKind",
			},
			matcher: BeFalse(),
		},
		{
			name:     "Empty owner reference",
			ownerRef: metav1.OwnerReference{},
			matcher:  BeFalse(),
		},
	}

	for i := range tests {
		tt := tests[i]
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := isLegacyOwnerRef(tt.ownerRef)
			g.Expect(result).To(tt.matcher)
		})
	}
}
