package features_test

import (
	"context"
	"path"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/integration/features/fixtures"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Applying and updating resources", func() {
	var (
		testNamespace   string
		namespace       *v1.Namespace
		objectCleaner   *envtestutil.Cleaner
		dsci            *dsciv1.DSCInitialization
		dummyAnnotation string
	)

	BeforeEach(func() {
		objectCleaner = envtestutil.CreateCleaner(envTestClient, envTest.Config, fixtures.Timeout, fixtures.Interval)

		testNamespace = "test-namespace"
		dummyAnnotation = "fake-anno"

		var err error
		namespace, err = cluster.CreateNamespace(envTestClient, testNamespace)
		Expect(err).ToNot(HaveOccurred())

		dsci = fixtures.NewDSCInitialization(testNamespace)
		dsci.Spec.ServiceMesh.ControlPlane.Namespace = namespace.Name
	})

	When("a feature is managed", func() {
		It("should reconcile the object to its managed state", func() {
			// given managed feature
			featuresHandler := createAndApplyFeature(dsci, true, "create-local-gw-svc", "local-gateway-svc.tmpl.yaml")

			// expect created svc to have managed annotation
			service := getServiceAndExpectAnnotations(envTestClient, testNamespace, "knative-local-gateway", map[string]string{
				"example-annotation":             "",
				annotations.ManagedByODHOperator: "true",
			})

			// modify managed service
			modifyAndExpectUpdate(envTestClient, service, "example-annotation", dummyAnnotation)

			// expect that modification is reconciled away
			Expect(featuresHandler.Apply()).To(Succeed())
			verifyAnnotation(envTestClient, testNamespace, service.Name, "example-annotation", "")
		})
	})

	When("a feature is unmanaged", func() {
		It("should not reconcile the object", func() {
			// given unmanaged feature
			featuresHandler := createAndApplyFeature(dsci, false, "create-local-gw-svc", "local-gateway-svc.tmpl.yaml")

			// modify unmanaged service object
			service, err := fixtures.GetService(envTestClient, testNamespace, "knative-local-gateway")
			Expect(err).ToNot(HaveOccurred())
			modifyAndExpectUpdate(envTestClient, service, "example-annotation", dummyAnnotation)

			// expect modification to remain after "reconcile"
			Expect(featuresHandler.Apply()).To(Succeed())
			verifyAnnotation(envTestClient, testNamespace, service.Name, "example-annotation", dummyAnnotation)
		})
	})

	When("a feature is unmanaged but the object is marked as managed", func() {
		It("should reconcile this object", func() {
			// given unmanaged feature but object marked with managed annotation
			featuresHandler := createAndApplyFeature(dsci, false, "create-managed-svc", "managed-svc.yaml")

			// expect service to have managed annotation
			service := getServiceAndExpectAnnotations(envTestClient, testNamespace, "managed-svc", map[string]string{
				"example-annotation":             "",
				annotations.ManagedByODHOperator: "true",
			})

			// modify managed service
			modifyAndExpectUpdate(envTestClient, service, "example-annotation", dummyAnnotation)

			// expect that modification is reconciled away
			Expect(featuresHandler.Apply()).To(Succeed())
			verifyAnnotation(envTestClient, testNamespace, service.Name, "example-annotation", "")
		})
	})

	AfterEach(func() {
		objectCleaner.DeleteAll(namespace)
	})
})

func createAndApplyFeature(dsci *dsciv1.DSCInitialization, managed bool, featureName, yamlFile string) *feature.FeaturesHandler {
	featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
		creator := feature.CreateFeature(featureName).
			For(handler).
			UsingConfig(envTest.Config).
			ManifestSource(fixtures.TestEmbeddedFiles).
			Manifests(path.Join(fixtures.BaseDir, yamlFile))
		if managed {
			creator.Managed()
		}
		return creator.Load()
	})
	Expect(featuresHandler.Apply()).To(Succeed())
	return featuresHandler
}

func getServiceAndExpectAnnotations(testClient client.Client, namespace, serviceName string, annotations map[string]string) *v1.Service {
	service, err := fixtures.GetService(testClient, namespace, serviceName)
	Expect(err).ToNot(HaveOccurred())
	for key, val := range annotations {
		Expect(service.Annotations[key]).To(Equal(val))
	}
	return service
}

func modifyAndExpectUpdate(client client.Client, service *v1.Service, annotationKey, newValue string) {
	if service.Annotations == nil {
		service.Annotations = make(map[string]string)
	}
	service.Annotations[annotationKey] = newValue
	Expect(client.Update(context.Background(), service)).To(Succeed())
}

func verifyAnnotation(client client.Client, namespace, serviceName, annotationKey, expectedValue string) {
	updatedService, err := fixtures.GetService(client, namespace, serviceName)
	Expect(err).ToNot(HaveOccurred())
	Expect(updatedService.Annotations[annotationKey]).To(Equal(expectedValue))
}
