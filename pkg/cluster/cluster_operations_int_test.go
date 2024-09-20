package cluster_test

import (
	"context"
	"time"

	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlruntime "sigs.k8s.io/controller-runtime/pkg/client"

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

		It("should create namespace if it does not exist", func(ctx context.Context) {
			// given
			namespace := envtestutil.AppendRandomNameTo("new-ns")
			defer objectCleaner.DeleteAll(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})

			// when
			ns, err := cluster.CreateNamespace(ctx, envTestClient, namespace)

			// then
			Expect(err).ToNot(HaveOccurred())
			Expect(ns.Status.Phase).To(Equal(corev1.NamespaceActive))
			Expect(ns.ObjectMeta.Generation).To(BeZero())
		})

		It("should not try to create namespace if it does already exist", func(ctx context.Context) {
			// given
			namespace := envtestutil.AppendRandomNameTo("existing-ns")
			newNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			}
			Expect(envTestClient.Create(ctx, newNamespace)).To(Succeed())
			defer objectCleaner.DeleteAll(ctx, newNamespace)

			// when
			existingNamespace, err := cluster.CreateNamespace(ctx, envTestClient, namespace)

			// then
			Expect(err).ToNot(HaveOccurred())
			Expect(existingNamespace).To(Equal(newNamespace))
		})

		It("should set labels", func(ctx context.Context) {
			// given
			namespace := envtestutil.AppendRandomNameTo("new-ns-with-labels")
			defer objectCleaner.DeleteAll(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})

			// when
			nsWithLabels, err := cluster.CreateNamespace(ctx, envTestClient, namespace, cluster.WithLabels("opendatahub.io/test-label", "true"))

			// then
			Expect(err).ToNot(HaveOccurred())
			Expect(nsWithLabels.Labels).To(HaveKeyWithValue("opendatahub.io/test-label", "true"))
		})

		It("should set annotations", func(ctx context.Context) {
			// given
			namespace := envtestutil.AppendRandomNameTo("new-ns-with-labels")
			defer objectCleaner.DeleteAll(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})

			// when
			nsWithLabels, err := cluster.CreateNamespace(ctx, envTestClient, namespace, cluster.WithAnnotations("opendatahub.io/test-annotation", "true"))

			// then
			Expect(err).ToNot(HaveOccurred())
			Expect(nsWithLabels.Annotations).To(HaveKeyWithValue("opendatahub.io/test-annotation", "true"))
		})

	})

	Context("Platform detection", func() {
		var objectCleaner *envtestutil.Cleaner
		operatorNS := "redhat-ods-operator"

		BeforeEach(func(ctx context.Context) {
			objectCleaner = envtestutil.CreateCleaner(envTestClient, envTest.Config, timeout, interval)
		})

		It("should run as unknown", func(ctx context.Context) {
			// given nothing
			// when
			platform, err := cluster.GetPlatform(ctx, envTestClient)
			Expect(err).ToNot(HaveOccurred())

			// then
			Expect(platform).To(Equal(cluster.Unknown))

		})

		It("should run as managed platform", func(ctx context.Context) {
			// give catalogsource exists in operator namespace
			_, errNs := cluster.CreateNamespace(ctx, envTestClient, operatorNS)
			Expect(errNs).ToNot(HaveOccurred())

			catalogSource := &ofapiv1alpha1.CatalogSource{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "addon-managed-odh-catalog",
					Namespace: operatorNS,
				},
			}
			Expect(envTestClient.Create(ctx, catalogSource)).To(Succeed())

			// when
			platform, err := cluster.GetPlatform(ctx, envTestClient)
			Expect(err).ToNot(HaveOccurred())
			defer objectCleaner.DeleteAll(ctx, catalogSource)

			// then
			Expect(platform).To(Equal(cluster.ManagedRhods))

		})

		It("should run as self-managed platform", func(ctx context.Context) {
			// given rhoai operatorcondition exist
			operatorCondition := &ofapiv2.OperatorCondition{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rhods-operator-something",
					Namespace: operatorNS,
				},
			}
			Expect(envTestClient.Create(ctx, operatorCondition)).To(Succeed())

			// when
			platform, err := cluster.GetPlatform(ctx, envTestClient)
			Expect(err).ToNot(HaveOccurred())
			defer objectCleaner.DeleteAll(ctx, operatorCondition)

			// then
			Expect(platform).To(Equal(cluster.SelfManagedRhods))

		})

		It("should run as ODH platform", func(ctx context.Context) {
			// given odh operatorcondition exist
			operatorCondition := &ofapiv2.OperatorCondition{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "opendatahub-operator-something",
					Namespace: operatorNS,
				},
			}
			Expect(envTestClient.Create(ctx, operatorCondition)).To(Succeed())

			// when
			platform, err := cluster.GetPlatform(ctx, envTestClient)
			Expect(err).ToNot(HaveOccurred())
			defer objectCleaner.DeleteAll(ctx, operatorCondition)

			// then
			Expect(platform).To(Equal(cluster.OpenDataHub))

		})
	})

	Context("config map manipulation", func() {

		var (
			objectCleaner *envtestutil.Cleaner
			namespace     string
			configMapMeta metav1.ObjectMeta
		)

		BeforeEach(func(ctx context.Context) {
			objectCleaner = envtestutil.CreateCleaner(envTestClient, envTest.Config, timeout, interval)
			namespace = envtestutil.AppendRandomNameTo("new-ns")
			configMapMeta = metav1.ObjectMeta{
				Name:      "config-regs",
				Namespace: namespace,
			}
			_, errNs := cluster.CreateNamespace(ctx, envTestClient, namespace)
			Expect(errNs).ToNot(HaveOccurred())
		})

		It("should create configmap with ns set through metaoptions", func(ctx context.Context) {
			// given
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "config-regs"},
				Data: map[string]string{
					"test-key": "test-value",
				},
			}

			// when
			err := cluster.CreateOrUpdateConfigMap(
				ctx,
				envTestClient,
				configMap,
				cluster.InNamespace(namespace),
			)
			Expect(err).ToNot(HaveOccurred())
			defer objectCleaner.DeleteAll(ctx, configMap)

			// then
			actualConfigMap := &corev1.ConfigMap{}
			Expect(envTestClient.Get(ctx, ctrlruntime.ObjectKeyFromObject(configMap), actualConfigMap)).To(Succeed())
			Expect(actualConfigMap.Namespace).To(Equal(namespace))
		})

		It("should create configmap with labels and owner reference", func(ctx context.Context) {
			// given
			configMap := &corev1.ConfigMap{
				ObjectMeta: configMapMeta,
				Data: map[string]string{
					"test-key": "test-value",
				},
			}

			// when
			err := cluster.CreateOrUpdateConfigMap(
				ctx,
				envTestClient,
				configMap,
				cluster.WithLabels(labels.K8SCommon.PartOf, "opendatahub"),
				cluster.WithOwnerReference(metav1.OwnerReference{
					APIVersion: "v1",
					Kind:       "Namespace",
					Name:       namespace,
					UID:        "random",
				}),
			)
			Expect(err).ToNot(HaveOccurred())
			defer objectCleaner.DeleteAll(ctx, configMap)

			// then
			actualConfigMap := &corev1.ConfigMap{}
			Expect(envTestClient.Get(ctx, ctrlruntime.ObjectKeyFromObject(configMap), actualConfigMap)).To(Succeed())
			Expect(actualConfigMap.Labels).To(HaveKeyWithValue(labels.K8SCommon.PartOf, "opendatahub"))
			getOwnerRefName := func(reference metav1.OwnerReference) string {
				return reference.Name
			}
			Expect(actualConfigMap.OwnerReferences[0]).To(WithTransform(getOwnerRefName, Equal(namespace)))
		})

		It("should be able to update existing config map", func(ctx context.Context) {
			// given
			createErr := cluster.CreateOrUpdateConfigMap(
				ctx,
				envTestClient,
				&corev1.ConfigMap{
					ObjectMeta: configMapMeta,
					Data: map[string]string{
						"test-key": "test-value",
					},
				},
				cluster.WithLabels("test-step", "create-configmap"),
			)
			Expect(createErr).ToNot(HaveOccurred())

			// when
			updatedConfigMap := &corev1.ConfigMap{
				ObjectMeta: configMapMeta,
				Data: map[string]string{
					"test-key": "new-value",
					"new-key":  "sth-new",
				},
			}

			updateErr := cluster.CreateOrUpdateConfigMap(
				ctx,
				envTestClient,
				updatedConfigMap,
				cluster.WithLabels("test-step", "update-existing-configmap"),
			)
			Expect(updateErr).ToNot(HaveOccurred())
			defer objectCleaner.DeleteAll(ctx, updatedConfigMap)

			// then
			actualConfigMap := &corev1.ConfigMap{}
			Expect(envTestClient.Get(ctx, ctrlruntime.ObjectKeyFromObject(updatedConfigMap), actualConfigMap)).To(Succeed())
			Expect(actualConfigMap.Data).To(HaveKeyWithValue("test-key", "new-value"))
			Expect(actualConfigMap.Data).To(HaveKeyWithValue("new-key", "sth-new"))
			Expect(actualConfigMap.Labels).To(HaveKeyWithValue("test-step", "update-existing-configmap"))
		})
	})

})
