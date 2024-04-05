package cluster_test

import (
	"context"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	timeout  = 5 * time.Second
	interval = 250 * time.Millisecond
)

var _ = Describe("Creating cluster resources", func() {

	Context("namespace creation", func() {

		var objectCleaner *envtestutil.Cleaner

		BeforeEach(func() {
			objectCleaner = envtestutil.CreateCleaner(envTestClient, envTest.Config, timeout, interval)
		})

		It("should create namespace if it does not exist", func() {
			// given
			namespace := envtestutil.AppendRandomNameTo("new-ns")
			defer objectCleaner.DeleteAll(&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})

			// when
			ns, err := cluster.CreateNamespace(envTestClient, namespace)

			// then
			Expect(err).ToNot(HaveOccurred())
			Expect(ns.Status.Phase).To(Equal(v1.NamespaceActive))
			Expect(ns.ObjectMeta.Generation).To(BeZero())
		})

		It("should not try to create namespace if it does already exist", func() {
			// given
			namespace := envtestutil.AppendRandomNameTo("existing-ns")
			newNamespace := &v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			}
			Expect(envTestClient.Create(context.Background(), newNamespace)).To(Succeed())
			defer objectCleaner.DeleteAll(newNamespace)

			// when
			existingNamespace, err := cluster.CreateNamespace(envTestClient, namespace)

			// then
			Expect(err).ToNot(HaveOccurred())
			Expect(existingNamespace).To(Equal(newNamespace))
		})

		It("should set labels", func() {
			// given
			namespace := envtestutil.AppendRandomNameTo("new-ns-with-labels")
			defer objectCleaner.DeleteAll(&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})

			// when
			nsWithLabels, err := cluster.CreateNamespace(envTestClient, namespace, cluster.WithLabels("opendatahub.io/test-label", "true"))

			// then
			Expect(err).ToNot(HaveOccurred())
			Expect(nsWithLabels.Labels).To(HaveKeyWithValue("opendatahub.io/test-label", "true"))
		})

	})

	Context("config map creation", func() {

		var objectCleaner *envtestutil.Cleaner

		BeforeEach(func() {
			objectCleaner = envtestutil.CreateCleaner(envTestClient, envTest.Config, timeout, interval)
		})

		It("should create configmap with labels and owner reference", func() {
			// given
			data := map[string]string{
				"test-key": "test-value",
			}

			// when
			configMap, err := cluster.CreateOrUpdateConfigMap(
				envTestClient,
				"config-regs",
				"default",
				data,
				cluster.WithLabels(labels.K8SCommon.PartOf, "opendatahub"),
				cluster.WithOwnerReference(metav1.OwnerReference{
					APIVersion: "v1",
					Kind:       "Namespace",
					Name:       "default",
					UID:        "default",
				}),
			)
			Expect(err).ToNot(HaveOccurred())
			defer objectCleaner.DeleteAll(configMap)

			// then
			Expect(configMap.Labels).To(HaveKeyWithValue(labels.K8SCommon.PartOf, "opendatahub"))
			getOwnerRefName := func(reference metav1.OwnerReference) string {
				return reference.Name
			}
			Expect(configMap.OwnerReferences[0]).To(WithTransform(getOwnerRefName, Equal("default")))
		})

		It("should be able to update existing config map", func() {
			// given
			data := map[string]string{
				"test-key": "test-value",
			}

			// when
			configMap, err := cluster.CreateOrUpdateConfigMap(
				envTestClient,
				"config-regs",
				"default",
				data,
			)
			Expect(err).ToNot(HaveOccurred())
			updatedConfigMap, err := cluster.CreateOrUpdateConfigMap(
				envTestClient,
				"config-regs",
				"default",
				map[string]string{
					"test-key": "new-value",
					"new-key":  "sth-new",
				},
			)
			Expect(err).ToNot(HaveOccurred())
			defer objectCleaner.DeleteAll(configMap)

			// then
			Expect(updatedConfigMap.Data).To(HaveKeyWithValue("test-key", "new-value"))
			Expect(updatedConfigMap.Data).To(HaveKeyWithValue("new-key", "sth-new"))

		})
	})

})
