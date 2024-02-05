package feature_test

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// AferoFsAdapter adapts an afero.Fs to fs.FS.
type AferoFsAdapter struct {
	Afs afero.Fs
}

// Open adapts the Open method to comply with fs.FS interface.
func (a AferoFsAdapter) Open(name string) (fs.File, error) {
	return a.Afs.Open(name)
}

var _ = Describe("Manifest Processing", func() {
	var (
		mockFS AferoFsAdapter
		path   string
	)

	BeforeEach(func() {
		fSys := afero.NewMemMapFs()
		mockFS = AferoFsAdapter{Afs: fSys}

	})

	Describe("baseManifest Process", func() {
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
			err := afero.WriteFile(mockFS.Afs, path, []byte(resourceYaml), 0644)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should process the manifest correctly", func() {
			// given
			manifest := feature.CreateBaseManifestFrom(mockFS, path)

			// when
			// Simulate adding to and processing from a slice of Manifest interfaces
			manifests := []feature.Manifest{manifest}
			var err error
			var objs []*unstructured.Unstructured
			for i := range manifests {
				objs, err = manifests[i].Process(nil)
				if err != nil {
					break
				}
			}
			Expect(err).NotTo(HaveOccurred())

			Expect(objs).To(HaveLen(1))
			Expect(objs[0].GetKind()).To(Equal("ConfigMap"))
			Expect(objs[0].GetName()).To(Equal("my-configmap"))
		})
	})

	Describe("TemplateManifest Process", func() {
		BeforeEach(func() {
			resourceYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-configmap
  namespace: {{.Namespace}}
data:
  key: Data
`
			path = "path/to/template.yaml"
			err := afero.WriteFile(mockFS.Afs, path, []byte(resourceYaml), 0644)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should process the template manifest correctly", func() {
			// given
			data := map[string]any{
				"Namespace": "template-ns",
			}
			manifest := feature.CreateTemplateManifestFrom(mockFS, path)

			// when
			// Simulate adding to and processing from a slice of Manifest interfaces
			manifests := []feature.Manifest{manifest}
			var err error
			var objs []*unstructured.Unstructured
			for i := range manifests {
				objs, err = manifests[i].Process(data)
				if err != nil {
					break
				}
			}
			Expect(err).NotTo(HaveOccurred())

			// then
			Expect(objs).To(HaveLen(1))
			Expect(objs[0].GetKind()).To(Equal("ConfigMap"))
			Expect(objs[0].GetName()).To(Equal("my-configmap"))
			Expect(objs[0].GetNamespace()).To(Equal("template-ns"))
		})

	})

	Describe("KustomizeManifest Process", func() {
		BeforeEach(func() {
			path = "/path/to/kustomization/" // base path here
		})

		It("should process the kustomize manifest correctly", func() {
			// given
			fSys := filesys.MakeFsInMemory()
			kustomizationYaml := `
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- resource.yaml
`
			resourceYaml := `
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-configmap
data:
  key: value
`
			// TODO: rework for map[string]any when supported
			data := feature.Spec{
				TargetNamespace: "kust-ns",
			}

			err := fSys.WriteFile(filepath.Join(path, "kustomization.yaml"), []byte(kustomizationYaml))
			Expect(err).ToNot(HaveOccurred())
			err = fSys.WriteFile(filepath.Join(path, "resource.yaml"), []byte(resourceYaml))
			Expect(err).ToNot(HaveOccurred())
			manifest := feature.CreateKustomizeManifestFrom("/path/to/kustomization/", fSys)

			// when
			manifests := []feature.Manifest{manifest}
			var objs []*unstructured.Unstructured
			for i := range manifests {
				objs, err = manifests[i].Process(&data)
				if err != nil {
					break
				}
			}
			Expect(err).NotTo(HaveOccurred())

			// then
			Expect(objs).To(HaveLen(1))
			configMap := objs[0]
			Expect(configMap.GetKind()).To(Equal("ConfigMap"))
			Expect(configMap.GetName()).To(Equal("my-configmap"))
		})
	})
})

func TestFeature(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Feature Suite")
}
