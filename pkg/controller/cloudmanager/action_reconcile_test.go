//nolint:testpackage // white-box tests for unexported filterCRs
package cloudmanager

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

func newUnstructuredObj(name string) unstructured.Unstructured {
	obj := unstructured.Unstructured{}
	obj.SetName(name)

	return obj
}

func TestFilterCRs(t *testing.T) {
	t.Run("no filter CRs returns all resources", func(t *testing.T) {
		g := NewWithT(t)

		resources := []unstructured.Unstructured{
			newUnstructuredObj("a"),
			newUnstructuredObj("b"),
			newUnstructuredObj("c"),
		}

		result := filterCRs(resources, nil)

		g.Expect(result).To(HaveLen(3))
	})

	t.Run("filters matching operator CR by GVK and name", func(t *testing.T) {
		g := NewWithT(t)

		istioCR := unstructured.Unstructured{}
		istioCR.SetGroupVersionKind(gvk.Istio)
		istioCR.SetName("default")

		resources := []unstructured.Unstructured{
			newUnstructuredObj("some-configmap"),
			istioCR,
			newUnstructuredObj("some-deployment"),
		}

		crs := []types.OperatorCR{
			{GVK: gvk.Istio, Name: "default"},
		}

		result := filterCRs(resources, crs)

		g.Expect(result).To(HaveLen(2))
		g.Expect(result[0].GetName()).To(Equal("some-configmap"))
		g.Expect(result[1].GetName()).To(Equal("some-deployment"))
	})

	t.Run("filters multiple operator CRs", func(t *testing.T) {
		g := NewWithT(t)

		istioCR := unstructured.Unstructured{}
		istioCR.SetGroupVersionKind(gvk.Istio)
		istioCR.SetName("default")

		certManagerCR := unstructured.Unstructured{}
		certManagerCR.SetGroupVersionKind(gvk.CertManagerV1Alpha1)
		certManagerCR.SetName("cluster")

		resources := []unstructured.Unstructured{
			newUnstructuredObj("keep-me"),
			istioCR,
			certManagerCR,
		}

		crs := []types.OperatorCR{
			{GVK: gvk.Istio, Name: "default"},
			{GVK: gvk.CertManagerV1Alpha1, Name: "cluster"},
		}

		result := filterCRs(resources, crs)

		g.Expect(result).To(HaveLen(1))
		g.Expect(result[0].GetName()).To(Equal("keep-me"))
	})

	t.Run("no match leaves resources unchanged", func(t *testing.T) {
		g := NewWithT(t)

		resources := []unstructured.Unstructured{
			newUnstructuredObj("a"),
			newUnstructuredObj("b"),
		}

		crs := []types.OperatorCR{
			{GVK: gvk.Istio, Name: "default"},
		}

		result := filterCRs(resources, crs)

		g.Expect(result).To(HaveLen(2))
	})
}
