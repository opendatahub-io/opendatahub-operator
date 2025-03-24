package manifest_test

import (
	"embed"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/manifest"

	. "github.com/onsi/gomega"
)

//go:embed resources/raw/*.yaml
//go:embed resources/template/*.yaml
var manifests embed.FS

func TestManifestProcessing(t *testing.T) {
	t.Run("Should Process the raw manifest with no substitutions", func(t *testing.T) {
		g := NewWithT(t)

		data := map[string]string{
			"TargetNamespace": "not-used",
		}

		objs, err := process(data, manifest.Create(manifests, "resources/raw/my-configmap.yaml"))

		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objs).To(HaveLen(1))
		g.Expect(objs[0].GetKind()).To(Equal("ConfigMap"))
		g.Expect(objs[0].GetName()).To(Equal("my-configmap"))
	})

	t.Run("Should fail when template refers to non existing key", func(t *testing.T) {
		g := NewWithT(t)

		data := map[string]string{
			"TargetNamespace": "template-ns",
		}

		_, err := process(data, manifest.Create(manifests, "resources/template/my-configmap.tmpl.yaml"))

		g.Expect(err).Should(MatchError(ContainSubstring("at <.Name>: map has no entry for key")))
	})

	t.Run("Should substitute target namespace in the templated manifest", func(t *testing.T) {
		g := NewWithT(t)

		data := map[string]string{
			"Name":            "my-configmap",
			"TargetNamespace": "template-ns",
		}

		objs, err := process(data, manifest.Create(manifests, "resources/template/my-configmap.tmpl.yaml"))

		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(objs).To(HaveLen(1))
		g.Expect(objs[0].GetKind()).To(Equal("ConfigMap"))
		g.Expect(objs[0].GetName()).To(Equal("my-configmap"))
		g.Expect(objs[0].GetNamespace()).To(Equal("template-ns"))
	})
}

func process(data any, m ...*manifest.Manifest) ([]*unstructured.Unstructured, error) {
	objs := make([]*unstructured.Unstructured, 0)
	for i := range m {
		r, err := m[i].Process(data)
		if err != nil {
			return nil, err
		}

		objs = append(objs, r...)
	}

	return objs, nil
}
