package servicemesh_test

import (
	"context"
	"path"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/gvr"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	timeout            = 5 * time.Second
	interval           = 250 * time.Millisecond
	authorinoName      = "authorino"
	authorinoNamespace = "test-provider"
)

var _ = Describe("Data Science Project Migration", func() {

	var (
		objectCleaner *envtestutil.Cleaner
		dsci          *dsciv1.DSCInitialization
	)

	BeforeEach(func() {
		objectCleaner = envtestutil.CreateCleaner(envTestClient, envTest.Config, timeout, interval)
		dsci = newDSCInitialization("default")
	})

	It("should migrate single namespace", func() {
		// given
		dataScienceNs := createDataScienceProject("dsp-01")
		regularNs := createNamespace("non-dsp")
		Expect(envTestClient.Create(context.Background(), dataScienceNs)).To(Succeed())
		Expect(envTestClient.Create(context.Background(), regularNs)).To(Succeed())
		defer objectCleaner.DeleteAll(dataScienceNs, regularNs)

		handler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
			return feature.CreateFeature("datascience-project-migration").
				For(handler).
				UsingConfig(envTest.Config).
				WithResources(servicemesh.MigratedDataScienceProjects).Load()
		})

		// when
		Expect(handler.Apply()).To(Succeed())

		// then
		Eventually(findMigratedNamespaces).
			WithTimeout(timeout).
			WithPolling(interval).
			Should(
				And(
					HaveLen(1),
					ContainElement("dsp-01"),
				),
			)
	})

	It("should not migrate any non-datascience namespace", func() {
		// given
		regularNs := createNamespace("non-dsp")
		Expect(envTestClient.Create(context.Background(), regularNs)).To(Succeed())
		defer objectCleaner.DeleteAll(regularNs)

		handler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
			return feature.CreateFeature("datascience-project-migration").
				For(handler).
				UsingConfig(envTest.Config).
				WithResources(servicemesh.MigratedDataScienceProjects).Load()
		})

		// when
		Expect(handler.Apply()).To(Succeed())

		// then
		Consistently(findMigratedNamespaces).
			WithTimeout(timeout).
			WithPolling(interval).
			Should(BeEmpty()) // we can't wait forever, but this should be good enough ;)
	})

	It("should migrate multiple namespaces", func() {
		// given
		dataScienceNs01 := createDataScienceProject("dsp-01")
		dataScienceNs02 := createDataScienceProject("dsp-02")
		dataScienceNs03 := createDataScienceProject("dsp-03")
		regularNs := createNamespace("non-dsp")
		Expect(envTestClient.Create(context.Background(), dataScienceNs01)).To(Succeed())
		Expect(envTestClient.Create(context.Background(), dataScienceNs02)).To(Succeed())
		Expect(envTestClient.Create(context.Background(), dataScienceNs03)).To(Succeed())
		Expect(envTestClient.Create(context.Background(), regularNs)).To(Succeed())
		defer objectCleaner.DeleteAll(dataScienceNs01, dataScienceNs02, dataScienceNs03, regularNs)

		handler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
			return feature.CreateFeature("datascience-project-migration").
				For(handler).
				UsingConfig(envTest.Config).
				WithResources(servicemesh.MigratedDataScienceProjects).Load()
		})

		// when
		Expect(handler.Apply()).To(Succeed())

		// then
		Eventually(findMigratedNamespaces).
			WithTimeout(timeout).
			WithPolling(interval).
			Should(
				And(
					HaveLen(3),
					ContainElements("dsp-01", "dsp-02", "dsp-03"),
				),
			)
	})

})

var _ = Describe("Cleanup operations", func() {

	// TODO combine with others
	Context("configuring control plane for auth(z)", func() {

		var (
			objectCleaner   *envtestutil.Cleaner
			dsci            *dsciv1.DSCInitialization
			serviceMeshSpec *infrav1.ServiceMeshSpec
			namespace       = "test"
			name            = "minimal"
		)

		BeforeEach(func() {
			objectCleaner = envtestutil.CreateCleaner(envTestClient, envTest.Config, timeout, interval)

			dsci = newDSCInitialization(namespace)

			serviceMeshSpec = &dsci.Spec.ServiceMesh

			serviceMeshSpec.ControlPlane.Name = name
			serviceMeshSpec.ControlPlane.Namespace = namespace
		})

		It("should be able to remove external provider on cleanup", func() {
			// given
			ns := createNamespace(namespace)
			Expect(envTestClient.Create(context.Background(), ns)).To(Succeed())
			defer objectCleaner.DeleteAll(ns)

			serviceMeshSpec.Auth.Namespace = authorinoNamespace
			serviceMeshSpec.Auth.Authorino.Name = authorinoName

			createServiceMeshControlPlane(name, namespace)

			handler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
				return feature.CreateFeature("control-plane-with-external-authz-provider").
					For(handler).
					Manifests(path.Join(feature.AuthDir, "mesh-authz-ext-provider.patch.tmpl")).
					OnDelete(servicemesh.RemoveExtensionProvider).
					UsingConfig(envTest.Config).
					Load()
			})

			// when
			By("verifying extension provider has been added after applying feature", func() {
				Expect(handler.Apply()).To(Succeed())
				serviceMeshControlPlane, err := getServiceMeshControlPlane(envTest.Config, namespace, name)
				Expect(err).ToNot(HaveOccurred())

				extensionProviders, found, err := unstructured.NestedSlice(serviceMeshControlPlane.Object, "spec", "techPreview", "meshConfig", "extensionProviders")
				Expect(err).ToNot(HaveOccurred())
				Expect(found).To(BeTrue())

				// TODO rework to nested fields
				extensionProvider := extensionProviders[0].(map[string]interface{})
				Expect(extensionProvider["name"]).To(Equal("test-odh-auth-provider"))
				Expect(extensionProvider["envoyExtAuthzGrpc"].(map[string]interface{})["service"]).To(Equal("authorino-authorino-authorization.test-provider.svc.cluster.local"))
			})

			// then
			By("verifying that extension provider has been removed", func() {
				Expect(handler.Delete()).To(Succeed())
				Eventually(func() []interface{} {

					serviceMeshControlPlane, err := getServiceMeshControlPlane(envTest.Config, namespace, name)
					Expect(err).ToNot(HaveOccurred())

					extensionProviders, found, err := unstructured.NestedSlice(serviceMeshControlPlane.Object, "spec", "techPreview", "meshConfig", "extensionProviders")
					Expect(err).ToNot(HaveOccurred())
					Expect(found).To(BeTrue())
					return extensionProviders

				}).WithTimeout(timeout).WithPolling(interval).Should(BeEmpty())
			})

		})

	})

})

func createServiceMeshControlPlane(name, namespace string) {
	serviceMeshControlPlane := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "maistra.io/v2",
			"kind":       "ServiceMeshControlPlane",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{},
		},
	}
	Expect(createSMCPInCluster(envTest.Config, serviceMeshControlPlane, namespace)).To(Succeed())
}

func createDataScienceProject(name string) *v1.Namespace {
	namespace := createNamespace(name)
	namespace.Labels = map[string]string{
		"opendatahub.io/dashboard": "true",
	}
	return namespace
}

func createNamespace(name string) *v1.Namespace {
	return &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func findMigratedNamespaces() []string {
	namespaces := &v1.NamespaceList{}
	var ns []string
	if err := envTestClient.List(context.Background(), namespaces); err != nil && !errors.IsNotFound(err) {
		Fail(err.Error())
	}
	for _, namespace := range namespaces.Items {
		if _, ok := namespace.ObjectMeta.Annotations["opendatahub.io/service-mesh"]; ok {
			ns = append(ns, namespace.Name)
		}
	}
	return ns
}

func newDSCInitialization(ns string) *dsciv1.DSCInitialization {
	return &dsciv1.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-dsci",
		},
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: ns,
		},
	}
}

// createSMCPInCluster uses dynamic client to create a dummy SMCP resource for testing.
func createSMCPInCluster(cfg *rest.Config, smcpObj *unstructured.Unstructured, namespace string) error {
	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return err
	}

	result, err := dynamicClient.Resource(gvr.SMCP).Namespace(namespace).Create(context.TODO(), smcpObj, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	statusConditions := []interface{}{
		map[string]interface{}{
			"type":   "Ready",
			"status": "True",
		},
	}

	// Since we don't have actual service mesh operator deployed, we simulate the status
	status := map[string]interface{}{
		"conditions": statusConditions,
		"readiness": map[string]interface{}{
			"components": map[string]interface{}{
				"pending": []interface{}{},
				"ready": []interface{}{
					"istiod",
					"ingress-gateway",
				},
				"unready": []interface{}{},
			},
		},
	}

	if err := unstructured.SetNestedField(result.Object, status, "status"); err != nil {
		return err
	}

	_, err = dynamicClient.Resource(gvr.SMCP).Namespace(namespace).UpdateStatus(context.TODO(), result, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	return nil
}

func getServiceMeshControlPlane(cfg *rest.Config, namespace, name string) (*unstructured.Unstructured, error) {
	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	smcp, err := dynamicClient.Resource(gvr.SMCP).Namespace(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return smcp, nil
}
