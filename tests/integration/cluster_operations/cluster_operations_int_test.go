package cluster_test

import (
	"context"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	timeout  = 5 * time.Second
	interval = 250 * time.Millisecond
)

var _ = Describe("Basic cluster operations", func() {

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

})
