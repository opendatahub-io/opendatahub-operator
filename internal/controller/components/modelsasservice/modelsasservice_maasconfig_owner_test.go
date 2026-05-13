//nolint:testpackage
package modelsasservice

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	odhlabels "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"

	. "github.com/onsi/gomega"
)

func uConfig(name string, lbl map[string]string) unstructured.Unstructured {
	return unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "maas.opendatahub.io/v1alpha1",
			"kind":       "Config",
			"metadata": map[string]interface{}{
				"name":   name,
				"labels": stringMapToAny(lbl),
			},
		},
	}
}

func stringMapToAny(m map[string]string) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func TestSelectMaasClusterConfig(t *testing.T) {
	g := NewWithT(t)
	partOf := odhlabels.K8SCommon.PartOf
	comp := odhlabels.ODH.Component(componentApi.ModelsAsServiceComponentName)
	val := componentApi.ModelsAsServiceComponentName

	t.Run("returns nil when empty", func(t *testing.T) {
		g.Expect(selectMaasClusterConfig(nil)).To(BeNil())
		g.Expect(selectMaasClusterConfig([]unstructured.Unstructured{})).To(BeNil())
	})

	t.Run("returns nil when multiple configs without part-of label", func(t *testing.T) {
		objs := []unstructured.Unstructured{
			uConfig("a", nil),
			uConfig("b", nil),
		}
		g.Expect(selectMaasClusterConfig(objs)).To(BeNil())
	})

	t.Run("returns singleton without part-of label", func(t *testing.T) {
		objs := []unstructured.Unstructured{uConfig("singleton", map[string]string{"other": "x"})}
		got := selectMaasClusterConfig(objs)
		g.Expect(got).ToNot(BeNil())
		g.Expect(got.GetName()).To(Equal("singleton"))
	})

	t.Run("prefers part-of match", func(t *testing.T) {
		objs := []unstructured.Unstructured{
			uConfig("unrelated", map[string]string{"other": "x"}),
			uConfig("maas", map[string]string{partOf: val}),
		}
		got := selectMaasClusterConfig(objs)
		g.Expect(got).ToNot(BeNil())
		g.Expect(got.GetName()).To(Equal("maas"))
	})

	t.Run("when multiple part-of picks component label true", func(t *testing.T) {
		objs := []unstructured.Unstructured{
			uConfig("first", map[string]string{partOf: val}),
			uConfig("second", map[string]string{partOf: val, comp: odhlabels.True}),
		}
		got := selectMaasClusterConfig(objs)
		g.Expect(got).ToNot(BeNil())
		g.Expect(got.GetName()).To(Equal("second"))
	})

	t.Run("when multiple part-of and no component label returns first part-of match", func(t *testing.T) {
		objs := []unstructured.Unstructured{
			uConfig("a", map[string]string{partOf: val}),
			uConfig("b", map[string]string{partOf: val}),
		}
		got := selectMaasClusterConfig(objs)
		g.Expect(got).ToNot(BeNil())
		g.Expect(got.GetName()).To(Equal("a"))
	})
}
