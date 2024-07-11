package kustomize_test

import (
	"path/filepath"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/plugins"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	kustomizationYaml = `
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
- resources.yaml
`
	resourceYaml = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-configmap
data:
  key: value
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-other-configmap
data:
  key: value
`
)

var _ = Describe("Kustomize support", func() {
	var (
		path      string
		inMemFsys filesys.FileSystem
	)

	BeforeEach(func() {
		path = "/path/to/kustomization/"
		inMemFsys = filesys.MakeFsInMemory()

		Expect(inMemFsys.WriteFile(filepath.Join(path, "kustomization.yaml"), []byte(kustomizationYaml))).To(Succeed())
		Expect(inMemFsys.WriteFile(filepath.Join(path, "resources.yaml"), []byte(resourceYaml))).To(Succeed())
	})

	It("should process the ConfigMap resource from the kustomize manifest", func() {
		// when
		objs := process(kustomize.Create(inMemFsys, path, plugins.CreateNamespaceApplierPlugin("kust-ns")))

		// then
		Expect(objs).To(HaveLen(2))
		configMap := objs[0]
		Expect(configMap.GetKind()).To(Equal("ConfigMap"))
		Expect(configMap.GetName()).To(Equal("my-configmap"))
		Expect(configMap.GetNamespace()).To(Equal("kust-ns"))
	})

	Context("using the builder", func() {

		It("should be able to enrich the kustomize builder with additional plugins before processing", func() {
			// given
			kustomizeBuilder := kustomize.Location(path).UsingFileSystem(inMemFsys)
			enricher := kustomize.PluginsEnricher{Plugins: []resmap.Transformer{plugins.CreateNamespaceApplierPlugin("kust-ns")}}
			enricher.Enrich(kustomizeBuilder)
			kustomization := kustomizeBuilder.Build()

			// when
			objs := process(kustomization)

			// then
			Expect(objs).To(HaveLen(2))
			configMap := objs[0]
			Expect(configMap.GetNamespace()).To(Equal("kust-ns"))
		})
	})

})

func process(m ...*kustomize.Kustomization) []*unstructured.Unstructured {
	var objs []*unstructured.Unstructured
	var err error
	for i := range m {
		objs, err = m[i].Process()
		if err != nil {
			break
		}
	}
	Expect(err).NotTo(HaveOccurred())
	return objs
}
