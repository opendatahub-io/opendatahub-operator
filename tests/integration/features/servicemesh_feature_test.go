package features_test

import (
	"context"
	"path"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/integration/features/fixtures"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Service Mesh setup", func() {

	var (
		dsci          *dsciv1.DSCInitialization
		objectCleaner *envtestutil.Cleaner
	)

	BeforeEach(func() {
		c, err := client.New(envTest.Config, client.Options{})
		Expect(err).ToNot(HaveOccurred())
		objectCleaner = envtestutil.CreateCleaner(c, envTest.Config, fixtures.Timeout, fixtures.Interval)

		namespace := envtestutil.AppendRandomNameTo("service-mesh-settings")

		dsci = fixtures.NewDSCInitialization(namespace)

		Expect(err).ToNot(HaveOccurred())
	})

	Describe("preconditions", func() {

		Context("operator setup", func() {

			When("operator is not installed", func() {

				It("should fail using precondition check", func() {
					// given
					featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
						verificationFeatureErr := feature.CreateFeature("no-service-mesh-operator-check").
							For(handler).
							UsingConfig(envTest.Config).
							PreConditions(servicemesh.EnsureServiceMeshOperatorInstalled).
							Load()

						Expect(verificationFeatureErr).ToNot(HaveOccurred())

						return nil
					})

					// when
					applyErr := featuresHandler.Apply()

					// then
					Expect(applyErr).To(MatchError(ContainSubstring("failed to find the pre-requisite operator subscription \"servicemeshoperator\"")))
				})
			})

			When("operator is installed", func() {
				var smcpCrdObj *apiextensionsv1.CustomResourceDefinition

				BeforeEach(func() {
					err := fixtures.CreateSubscription(envTestClient, "openshift-operators", fixtures.OssmSubscription)
					Expect(err).ToNot(HaveOccurred())
					smcpCrdObj = installServiceMeshCRD()
				})

				AfterEach(func() {
					objectCleaner.DeleteAll(smcpCrdObj)
				})

				It("should succeed using precondition check", func() {
					// when
					featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
						verificationFeatureErr := feature.CreateFeature("service-mesh-operator-check").
							For(handler).
							UsingConfig(envTest.Config).
							PreConditions(servicemesh.EnsureServiceMeshOperatorInstalled).
							Load()

						Expect(verificationFeatureErr).ToNot(HaveOccurred())

						return nil
					})

					// when
					Expect(featuresHandler.Apply()).To(Succeed())

				})

				It("should find installed Service Mesh Control Plane", func() {
					// given
					c, err := client.New(envTest.Config, client.Options{})
					Expect(err).ToNot(HaveOccurred())

					ns := envtestutil.AppendRandomNameTo(fixtures.TestNamespacePrefix)
					nsResource := fixtures.NewNamespace(ns)
					Expect(c.Create(context.Background(), nsResource)).To(Succeed())
					defer objectCleaner.DeleteAll(nsResource)

					createServiceMeshControlPlane("test-name", ns)
					dsci.Spec.ServiceMesh.ControlPlane.Namespace = ns
					dsci.Spec.ServiceMesh.ControlPlane.Name = "test-name"

					// when
					featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
						verificationFeatureErr := feature.CreateFeature("service-mesh-control-plane-check").
							For(handler).
							UsingConfig(envTest.Config).
							PreConditions(servicemesh.EnsureServiceMeshInstalled).
							Load()

						Expect(verificationFeatureErr).ToNot(HaveOccurred())

						return nil
					})

					// then
					Expect(featuresHandler.Apply()).To(Succeed())
				})

				It("should fail to find Service Mesh Control Plane if not present", func() {
					// given
					dsci.Spec.ServiceMesh.ControlPlane.Name = "test-name"

					// when
					featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
						verificationFeatureErr := feature.CreateFeature("no-service-mesh-control-plane-check").
							For(handler).
							UsingConfig(envTest.Config).
							PreConditions(servicemesh.EnsureServiceMeshInstalled).
							Load()

						Expect(verificationFeatureErr).ToNot(HaveOccurred())

						return nil
					})

					// then
					Expect(featuresHandler.Apply()).To(MatchError(ContainSubstring("failed to find Service Mesh Control Plane")))
				})

			})
		})

		Context("Control Plane configuration", func() {

			When("setting up auth(z) provider", func() {

				var (
					objectCleaner   *envtestutil.Cleaner
					dsci            *dsciv1.DSCInitialization
					serviceMeshSpec *infrav1.ServiceMeshSpec
					smcpCrdObj      *apiextensionsv1.CustomResourceDefinition
					namespace       = "test-ns"
					name            = "minimal"
				)

				BeforeEach(func() {
					smcpCrdObj = installServiceMeshCRD()
					objectCleaner = envtestutil.CreateCleaner(envTestClient, envTest.Config, fixtures.Timeout, fixtures.Interval)
					dsci = fixtures.NewDSCInitialization(namespace)

					serviceMeshSpec = dsci.Spec.ServiceMesh

					serviceMeshSpec.ControlPlane.Name = name
					serviceMeshSpec.ControlPlane.Namespace = namespace
				})

				AfterEach(func() {
					objectCleaner.DeleteAll(smcpCrdObj)
				})

				It("should be able to remove external provider on cleanup", func() {
					// given
					ns := fixtures.NewNamespace(namespace)
					Expect(envTestClient.Create(context.Background(), ns)).To(Succeed())
					defer objectCleaner.DeleteAll(ns)

					serviceMeshSpec.Auth.Namespace = "auth-provider"

					createServiceMeshControlPlane(name, namespace)

					handler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
						return feature.CreateFeature("control-plane-with-external-authz-provider").
							For(handler).
							ManifestsLocation(fixtures.TestEmbeddedFiles).
							Manifests(path.Join("templates", "mesh-authz-ext-provider.patch.tmpl.yaml")).
							OnDelete(
								servicemesh.RemoveExtensionProvider,
							).
							UsingConfig(envTest.Config).
							Load()
					})

					// when
					By("verifying extension provider has been added after applying feature", func() {
						Expect(handler.Apply()).To(Succeed())
						serviceMeshControlPlane, err := getServiceMeshControlPlane(namespace, name)
						Expect(err).ToNot(HaveOccurred())

						extensionProviders, found, err := unstructured.NestedSlice(serviceMeshControlPlane.Object, "spec", "techPreview", "meshConfig", "extensionProviders")
						Expect(err).ToNot(HaveOccurred())
						Expect(found).To(BeTrue())

						extensionProvider, ok := extensionProviders[0].(map[string]interface{})
						if !ok {
							Fail("extension provider has not been added after applying feature")
						}
						Expect(extensionProvider["name"]).To(Equal("test-ns-auth-provider"))

						envoyExtAuthzGrpc, ok := extensionProvider["envoyExtAuthzGrpc"].(map[string]interface{})
						if !ok {
							Fail("extension provider envoyExtAuthzGrpc has not been added after applying feature")
						}
						Expect(envoyExtAuthzGrpc["service"]).To(Equal("authorino-authorino-authorization.auth-provider.svc.cluster.local"))
					})

					// then
					By("verifying that extension provider has been removed and namespace is gone too", func() {
						Expect(handler.Delete()).To(Succeed())
						Eventually(func() []interface{} {

							serviceMeshControlPlane, err := getServiceMeshControlPlane(namespace, name)
							Expect(err).ToNot(HaveOccurred())

							extensionProviders, found, err := unstructured.NestedSlice(serviceMeshControlPlane.Object, "spec", "techPreview", "meshConfig", "extensionProviders")
							Expect(err).ToNot(HaveOccurred())
							Expect(found).To(BeTrue())

							_, err = fixtures.GetNamespace(envTestClient, serviceMeshSpec.Auth.Namespace)
							Expect(errors.IsNotFound(err)).To(BeTrue())

							return extensionProviders

						}).WithTimeout(fixtures.Timeout).WithPolling(fixtures.Interval).Should(BeEmpty())
					})

				})

			})

		})

	})
})

func installServiceMeshCRD() *apiextensionsv1.CustomResourceDefinition {
	smcpCrdObj := &apiextensionsv1.CustomResourceDefinition{}
	Expect(yaml.Unmarshal([]byte(fixtures.ServiceMeshControlPlaneCRD), smcpCrdObj)).ToNot(HaveOccurred())
	Expect(envTestClient.Create(context.TODO(), smcpCrdObj)).ToNot(HaveOccurred())

	crdOptions := envtest.CRDInstallOptions{PollInterval: fixtures.Interval, MaxTime: fixtures.Timeout}
	Expect(envtest.WaitForCRDs(envTest.Config, []*apiextensionsv1.CustomResourceDefinition{smcpCrdObj}, crdOptions)).To(Succeed())

	return smcpCrdObj
}

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
	Expect(createSMCPInCluster(serviceMeshControlPlane, namespace)).To(Succeed())
}

func createSMCPInCluster(smcpObj *unstructured.Unstructured, namespace string) error {
	smcpObj.SetGroupVersionKind(gvk.ServiceMeshControlPlane)
	smcpObj.SetNamespace(namespace)
	if err := envTestClient.Create(context.TODO(), smcpObj); err != nil {
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
	update := smcpObj.DeepCopy()
	if err := unstructured.SetNestedField(update.Object, status, "status"); err != nil {
		return err
	}

	return envTestClient.Status().Update(context.TODO(), update)
}

func getServiceMeshControlPlane(namespace, name string) (*unstructured.Unstructured, error) {
	smcpObj := &unstructured.Unstructured{}
	smcpObj.SetGroupVersionKind(gvk.ServiceMeshControlPlane)

	err := envTestClient.Get(context.TODO(), client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, smcpObj)

	return smcpObj, err
}
