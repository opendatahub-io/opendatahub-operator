package manifest_test

import (
	"io/fs"
	"path/filepath"

	"github.com/spf13/afero"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/manifest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Manifest Processing", func() {
	var (
		inMemFS AferoFsAdapter
		path    string
	)

	BeforeEach(func() {
		inMemFS = AferoFsAdapter{afero.NewMemMapFs()}
	})

	Describe("Raw Manifest Processing", func() {
		BeforeEach(func() {
			resourceYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
 name: my-configmap
 namespace: fake-ns
data:
 key: value
`
			path = "path/to/test.yaml"
			err := afero.WriteFile(inMemFS.Fs, path, []byte(resourceYaml), 0644)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should Process the raw manifest with no substitutions", func() {
			// given
			data := struct{ TargetNamespace string }{
				TargetNamespace: "not-used",
			}

			// when
			// Simulate adding to and processing from a slice of Manifest interfaces
			objs := process(data, manifest.Create(inMemFS, path))

			Expect(objs).To(HaveLen(1))
			Expect(objs[0].GetKind()).To(Equal("ConfigMap"))
			Expect(objs[0].GetName()).To(Equal("my-configmap"))
		})
	})

	Describe("Templated Manifest Processing", func() {
		resourceYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-configmap
  namespace: {{.TargetNamespace}}
data:
  key: FeatureContext
`
		BeforeEach(func() {
			path = "path/to/template.tmpl.yaml"
			err := afero.WriteFile(inMemFS.Fs, path, []byte(resourceYaml), 0644)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should fail when template refers to non existing key", func() {
			// given
			pathToBrokenTpl := filepath.Join("broken", path)
			Expect(afero.WriteFile(inMemFS.Fs, pathToBrokenTpl, []byte(resourceYaml+"\n {{ .NotExistingKey }}"), 0644)).To(Succeed())
			data := map[string]string{
				"TargetNamespace": "template-ns",
			}
			manifest := manifest.Create(inMemFS, pathToBrokenTpl)

			// when
			_, err := manifest.Process(data)

			// then
			Expect(err).Should(MatchError(ContainSubstring("at <.NotExistingKey>: map has no entry for key")))
		})

		It("should substitute target namespace in the templated manifest", func() {
			// given
			data := struct{ TargetNamespace string }{
				TargetNamespace: "template-ns",
			}

			// when
			// Simulate adding to and processing from a slice of Manifest interfaces
			objs := process(data, manifest.Create(inMemFS, path))

			// then
			Expect(objs).To(HaveLen(1))
			Expect(objs[0].GetKind()).To(Equal("ConfigMap"))
			Expect(objs[0].GetName()).To(Equal("my-configmap"))
			Expect(objs[0].GetNamespace()).To(Equal("template-ns"))
		})

	})

})

func process(data any, m ...*manifest.Manifest) []*unstructured.Unstructured {
	var objs []*unstructured.Unstructured
	var err error
	for i := range m {
		objs, err = m[i].Process(data)
		if err != nil {
			break
		}
	}
	Expect(err).NotTo(HaveOccurred())
	return objs
}

var _ fs.FS = (*AferoFsAdapter)(nil)

type AferoFsAdapter struct {
	afero.Fs
}

// Open adapts the Open method to comply with fs.FS interface.
func (a AferoFsAdapter) Open(name string) (fs.File, error) {
	return a.Fs.Open(name)
}
