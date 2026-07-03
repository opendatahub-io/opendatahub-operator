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

	t.Run("filters cluster-scoped CR by GVK and name", func(t *testing.T) {
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

	t.Run("filters namespaced resource by GVK, namespace, and name", func(t *testing.T) {
		g := NewWithT(t)

		dep := unstructured.Unstructured{}
		dep.SetGroupVersionKind(gvk.Deployment)
		dep.SetName("my-deploy")
		dep.SetNamespace("ns-a")

		resources := []unstructured.Unstructured{
			newUnstructuredObj("some-configmap"),
			dep,
		}

		crs := []types.OperatorCR{
			{GVK: gvk.Deployment, Name: "my-deploy", Namespace: "ns-a"},
		}

		result := filterCRs(resources, crs)

		g.Expect(result).To(HaveLen(1))
		g.Expect(result[0].GetName()).To(Equal("some-configmap"))
	})

	t.Run("does not filter resource when namespace differs", func(t *testing.T) {
		g := NewWithT(t)

		dep := unstructured.Unstructured{}
		dep.SetGroupVersionKind(gvk.Deployment)
		dep.SetName("my-deploy")
		dep.SetNamespace("ns-b")

		resources := []unstructured.Unstructured{
			newUnstructuredObj("some-configmap"),
			dep,
		}

		crs := []types.OperatorCR{
			{GVK: gvk.Deployment, Name: "my-deploy", Namespace: "ns-a"},
		}

		result := filterCRs(resources, crs)

		g.Expect(result).To(HaveLen(2))
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

	t.Run("cluster-scoped CR (no namespace) filters resource whose Helm template sets a namespace", func(t *testing.T) {
		// The sail-operator chart sets metadata.namespace on the Istio CR even though
		// Istio is cluster-scoped. The k8s-manifest-kit renderer preserves that field
		// verbatim, so GetNamespace() returns "istio-system" on the rendered object.
		// filterCRs must still filter it when OperatorCR.Namespace is "".
		g := NewWithT(t)

		istioCR := unstructured.Unstructured{}
		istioCR.SetGroupVersionKind(gvk.Istio)
		istioCR.SetName("default")
		istioCR.SetNamespace("istio-system")

		resources := []unstructured.Unstructured{
			newUnstructuredObj("keep-me"),
			istioCR,
		}

		crs := []types.OperatorCR{
			{GVK: gvk.Istio, Name: "default"},
		}

		result := filterCRs(resources, crs)

		g.Expect(result).To(HaveLen(1))
		g.Expect(result[0].GetName()).To(Equal("keep-me"))
	})

	t.Run("namespace-scoped CR with Namespace set does not match resource in different namespace", func(t *testing.T) {
		// Guard: the cluster-scoped namespace-skip logic (see test above) must not bleed into
		// namespace-scoped CRs. When OperatorCR.Namespace is set, the namespace must still match exactly.
		g := NewWithT(t)

		dep := unstructured.Unstructured{}
		dep.SetGroupVersionKind(gvk.Deployment)
		dep.SetName("my-deploy")
		dep.SetNamespace("ns-b")

		resources := []unstructured.Unstructured{dep}

		crs := []types.OperatorCR{
			{GVK: gvk.Deployment, Name: "my-deploy", Namespace: "ns-a"},
		}

		result := filterCRs(resources, crs)

		g.Expect(result).To(HaveLen(1))
	})
}
