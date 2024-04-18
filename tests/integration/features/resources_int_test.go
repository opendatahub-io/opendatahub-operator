package features_test

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/integration/features/fixtures"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Creating and updating resources", func() {
	var (
		testNamespace string
		testObjects   []*unstructured.Unstructured
		metaOptions   []cluster.MetaOptions
	)

	BeforeEach(func() {
		testNamespace = "test-namespace"

		cm := &unstructured.Unstructured{}
		cm.SetAPIVersion("v1")
		cm.SetKind("ConfigMap")
		cm.SetName("test-configmap")
		cm.SetNamespace(testNamespace)
		cm.SetAnnotations(map[string]string{
			annotations.ManagedByODHOperator: "true",
		})

		testObjects = []*unstructured.Unstructured{cm}

		ns := fixtures.NewNamespace(testNamespace)
		err := fixtures.CreateOrUpdateNamespace(envTestClient, ns)
		Expect(err).NotTo(HaveOccurred())
	})

	When("an object does not exist", func() {
		It("should create the object", func() {
			err := feature.CreateResources(envTestClient, testObjects, metaOptions...)
			Expect(err).NotTo(HaveOccurred())

			// Check if object has been created
			cm, err := fixtures.GetConfigMap(envTestClient, testNamespace, "test-configmap")
			Expect(err).NotTo(HaveOccurred())
			Expect(cm).NotTo(BeNil())
		})
	})

	When("an object exists and is managed", func() {
		It("should update the existing object", func() {
			initialCM := testObjects[0].DeepCopy()
			Expect(envTestClient.Create(context.Background(), initialCM)).To(Succeed())

			testObjects[0].Object["data"] = map[string]interface{}{
				"key1": "updatedValue",
			}

			// createResources should update this object
			err := feature.CreateResources(envTestClient, testObjects, metaOptions...)
			Expect(err).NotTo(HaveOccurred())

			cm, err := fixtures.GetConfigMap(envTestClient, testNamespace, "test-configmap")
			Expect(err).NotTo(HaveOccurred())
			Expect(cm.Data["key1"]).To(Equal("updatedValue"))
		})
	})

	When("an object exists and is unmanaged", func() {
		It("should not update the existing object", func() {
			initialCM := testObjects[0]
			initialCM.SetAnnotations(map[string]string{
				annotations.ManagedByODHOperator: "false",
			})
			Expect(envTestClient.Create(context.Background(), initialCM)).To(Succeed())

			testObjects[0].Object["data"] = map[string]interface{}{
				"key1": "updatedValue",
			}

			// createResources should not update this object
			err := feature.CreateResources(envTestClient, testObjects, metaOptions...)
			Expect(err).NotTo(HaveOccurred())

			cm, err := fixtures.GetConfigMap(envTestClient, testNamespace, "test-configmap")
			Expect(err).NotTo(HaveOccurred())
			Expect(cm.Data["key1"]).To(Equal(""))
		})
	})

	When("the a metaOption function fails", func() {
		It("should return the error", func() {
			failingMetaOption := func(o metav1.Object) error {
				return fmt.Errorf("simulated meta option error")
			}

			metaOptions = append(metaOptions, failingMetaOption)

			err := feature.CreateResources(envTestClient, testObjects, metaOptions...)
			Expect(err).To(HaveOccurred())
		})
	})

	AfterEach(func() {
		for _, obj := range testObjects {
			err := envTestClient.Delete(context.Background(), obj)
			if err != nil {
				return
			}
		}
	})
})
