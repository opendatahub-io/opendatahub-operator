package feature_test

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ fs.FS = (*AferoFsAdapter)(nil)

type AferoFsAdapter struct {
	Afs afero.Fs
}

// Open adapts the Open method to comply with fs.FS interface.
func (a AferoFsAdapter) Open(name string) (fs.File, error) {
	return a.Afs.Open(name)
}

var _ = Describe("Manifest Processing", func() {
	var (
		inMemFS  AferoFsAdapter
		path     string
		kustFsys filesys.FileSystem
	)

	BeforeEach(func() {
		fSys := afero.NewMemMapFs()
		inMemFS = AferoFsAdapter{Afs: fSys}

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
			err := afero.WriteFile(inMemFS.Afs, path, []byte(resourceYaml), 0644)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should process the base manifest with no substitutions", func() {
			// given
			manifest := feature.CreateBaseManifestFrom(inMemFS, path)

			data := feature.Spec{
				TargetNamespace: "not-used",
			}

			// when
			// Simulate adding to and processing from a slice of Manifest interfaces
			manifests := []feature.Manifest{manifest}
			objs := processManifests(data, manifests)

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
  key: Data
`

		BeforeEach(func() {
			path = "path/to/template.yaml"
			err := afero.WriteFile(inMemFS.Afs, path, []byte(resourceYaml), 0644)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should fail when template refers to non existing key", func() {
			// given
			pathToBrokenTpl := filepath.Join("broken", path)
			Expect(afero.WriteFile(inMemFS.Afs, pathToBrokenTpl, []byte(resourceYaml+"\n {{ .NotExistingKey }}"), 0644)).To(Succeed())
			data := map[string]string{
				"TargetNamespace": "template-ns",
			}
			manifest := feature.CreateTemplateManifestFrom(inMemFS, pathToBrokenTpl)

			// when
			_, err := manifest.Process(data)

			// then
			Expect(err).Should(MatchError(ContainSubstring("at <.NotExistingKey>: map has no entry for key")))
		})

		It("should substitute target namespace in the templated manifest", func() {
			// given
			data := feature.Spec{
				TargetNamespace: "template-ns",
			}
			manifest := feature.CreateTemplateManifestFrom(inMemFS, path)

			// when
			// Simulate adding to and processing from a slice of Manifest interfaces
			manifests := []feature.Manifest{manifest}
			objs := processManifests(data, manifests)

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
			kustFsys = filesys.MakeFsInMemory()
		})

		It("should process the ConfigMap resource from the kustomize manifest", func() {
			// given
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
			data := feature.Spec{
				TargetNamespace: "kust-ns",
			}

			Expect(kustFsys.WriteFile(filepath.Join(path, "kustomization.yaml"), []byte(kustomizationYaml))).To(Succeed())
			Expect(kustFsys.WriteFile(filepath.Join(path, "resource.yaml"), []byte(resourceYaml))).To(Succeed())
			manifest := feature.CreateKustomizeManifestFrom("/path/to/kustomization/", kustFsys)

			// when
			manifests := []feature.Manifest{manifest}
			objs := processManifests(data, manifests)

			// then
			Expect(objs).To(HaveLen(1))
			configMap := objs[0]
			Expect(configMap.GetKind()).To(Equal("ConfigMap"))
			Expect(configMap.GetName()).To(Equal("my-configmap"))
		})
	})
})

func processManifests(data feature.Spec, m []feature.Manifest) []*unstructured.Unstructured {
	var objs []*unstructured.Unstructured
	var err error
	for i := range m {
		objs, err = m[i].Process(&data)
		if err != nil {
			break
		}
	}
	Expect(err).NotTo(HaveOccurred())
	return objs
}

func TestManifests(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Manifest Process Suite")
}
