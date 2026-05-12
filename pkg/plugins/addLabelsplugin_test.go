package plugins_test

import (
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/api/resource"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/plugins"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Label plugins", func() {
	var (
		deploymentRes *resource.Resource
		serviceRes    *resource.Resource
		resMap        resmap.ResMap
		labels        map[string]string
	)

	BeforeEach(func() {
		var err error
		deploymentRes, err = factory.FromBytes([]byte(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-deployment
spec:
  selector:
    matchLabels:
      app: test
  template:
    metadata:
      labels:
        app: test
    spec:
      containers:
      - name: main
        image: test:latest
`))
		Expect(err).NotTo(HaveOccurred())

		serviceRes, err = factory.FromBytes([]byte(`
apiVersion: v1
kind: Service
metadata:
  name: test-service
spec:
  selector:
    app: test
  ports:
  - port: 80
`))
		Expect(err).NotTo(HaveOccurred())

		labels = map[string]string{
			"component": "maas",
			"part-of":   "test-system",
		}
	})

	Describe("CreateSetLabelsPlugin", func() {
		It("should add labels to metadata, selector/matchLabels, and template labels on Deployments", func() {
			resMap = resmap.New()
			Expect(resMap.Append(deploymentRes)).To(Succeed())

			plugin := plugins.CreateSetLabelsPlugin(labels)
			Expect(plugin.Transform(resMap)).To(Succeed())

			result := resMap.Resources()[0]
			yaml := result.MustYaml()

			Expect(yaml).To(ContainSubstring("component: maas"))
			Expect(yaml).To(ContainSubstring("part-of: test-system"))

			matchLabels, err := result.GetFieldValue("spec.selector.matchLabels")
			Expect(err).NotTo(HaveOccurred())
			Expect(matchLabels).To(HaveKeyWithValue("component", "maas"))
		})

		It("should add labels to metadata on non-Deployment resources", func() {
			resMap = resmap.New()
			Expect(resMap.Append(serviceRes)).To(Succeed())

			plugin := plugins.CreateSetLabelsPlugin(labels)
			Expect(plugin.Transform(resMap)).To(Succeed())

			result := resMap.Resources()[0]
			metadataLabels, err := result.GetFieldValue("metadata.labels")
			Expect(err).NotTo(HaveOccurred())
			Expect(metadataLabels).To(HaveKeyWithValue("component", "maas"))
		})
	})

	Describe("CreateMetadataOnlyLabelsPlugin", func() {
		It("should add labels to metadata and template labels but NOT selector/matchLabels", func() {
			resMap = resmap.New()
			Expect(resMap.Append(deploymentRes)).To(Succeed())

			plugin := plugins.CreateMetadataOnlyLabelsPlugin(labels)
			Expect(plugin.Transform(resMap)).To(Succeed())

			result := resMap.Resources()[0]

			metadataLabels, err := result.GetFieldValue("metadata.labels")
			Expect(err).NotTo(HaveOccurred())
			Expect(metadataLabels).To(HaveKeyWithValue("component", "maas"))

			templateLabels, err := result.GetFieldValue("spec.template.metadata.labels")
			Expect(err).NotTo(HaveOccurred())
			Expect(templateLabels).To(HaveKeyWithValue("component", "maas"))

			matchLabels, err := result.GetFieldValue("spec.selector.matchLabels")
			Expect(err).NotTo(HaveOccurred())
			Expect(matchLabels).NotTo(HaveKey("component"),
				"CreateMetadataOnlyLabelsPlugin must not touch spec.selector.matchLabels")
			Expect(matchLabels).NotTo(HaveKey("part-of"))
		})

		It("should add labels to metadata on non-Deployment resources", func() {
			resMap = resmap.New()
			Expect(resMap.Append(serviceRes)).To(Succeed())

			plugin := plugins.CreateMetadataOnlyLabelsPlugin(labels)
			Expect(plugin.Transform(resMap)).To(Succeed())

			result := resMap.Resources()[0]
			metadataLabels, err := result.GetFieldValue("metadata.labels")
			Expect(err).NotTo(HaveOccurred())
			Expect(metadataLabels).To(HaveKeyWithValue("component", "maas"))
		})

		It("should preserve existing selector labels unchanged", func() {
			resMap = resmap.New()
			Expect(resMap.Append(deploymentRes)).To(Succeed())

			plugin := plugins.CreateMetadataOnlyLabelsPlugin(labels)
			Expect(plugin.Transform(resMap)).To(Succeed())

			result := resMap.Resources()[0]
			matchLabels, err := result.GetFieldValue("spec.selector.matchLabels")
			Expect(err).NotTo(HaveOccurred())
			Expect(matchLabels).To(HaveKeyWithValue("app", "test"),
				"original selector labels should be preserved")
		})
	})
})
