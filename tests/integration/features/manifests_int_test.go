package features_test

import (
	"os"
	"path"

	corev1 "k8s.io/api/core/v1"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/integration/features/fixtures"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Manifest sources", func() {

	var (
		objectCleaner *envtestutil.Cleaner
		dsci          *dsciv1.DSCInitialization
		namespace     *corev1.Namespace
	)

	BeforeEach(func() {
		objectCleaner = envtestutil.CreateCleaner(envTestClient, envTest.Config, fixtures.Timeout, fixtures.Interval)
		nsName := envtestutil.AppendRandomNameTo("smcp-ns")

		var err error
		namespace, err = cluster.CreateNamespaceIfNotExists(envTestClient, nsName)
		Expect(err).ToNot(HaveOccurred())

		dsci = fixtures.NewDSCInitialization(nsName)
		dsci.Spec.ServiceMesh.ControlPlane.Namespace = namespace.Name
	})

	AfterEach(func() {
		objectCleaner.DeleteAll(namespace)
	})

	It("should be able to process an embedded template from the default location", func() {
		// given
		featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
			createServiceErr := feature.CreateFeature("create-local-gw-svc").
				For(handler).
				UsingConfig(envTest.Config).
				Manifests(path.Join(feature.ServerlessDir, "serving-istio-gateways", "local-gateway-svc.tmpl")).
				Load()

			Expect(createServiceErr).ToNot(HaveOccurred())

			return nil
		})

		// when
		Expect(featuresHandler.Apply()).To(Succeed())

		// then
		service, err := fixtures.GetService(envTestClient, namespace.Name, "knative-local-gateway")
		Expect(err).ToNot(HaveOccurred())
		Expect(service.Name).To(Equal("knative-local-gateway"))
	})

	It("should be able to process an embedded YAML file from the default location", func() {
		// given
		knativeNs, nsErr := cluster.CreateNamespaceIfNotExists(envTestClient, "knative-serving")
		Expect(nsErr).ToNot(HaveOccurred())
		defer objectCleaner.DeleteAll(knativeNs)

		featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
			createGatewayErr := feature.CreateFeature("create-local-gateway").
				For(handler).
				UsingConfig(envTest.Config).
				Manifests(path.Join(feature.ServerlessDir, "serving-istio-gateways", "istio-local-gateway.yaml")).
				Load()

			Expect(createGatewayErr).ToNot(HaveOccurred())

			return nil
		})

		// when
		Expect(featuresHandler.Apply()).To(Succeed())

		// then
		gateway, err := getGateway(envTest.Config, "knative-serving", "knative-local-gateway")
		Expect(err).ToNot(HaveOccurred())
		Expect(gateway).ToNot(BeNil())
	})

	It("should be able to process an embedded file from a non default location", func() {
		// given
		featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
			createNamespaceErr := feature.CreateFeature("create-namespace").
				For(handler).
				UsingConfig(envTest.Config).
				ManifestSource(fixtures.TestEmbeddedFiles).
				Manifests(path.Join(feature.BaseDir, "namespace.yaml")).
				Load()

			Expect(createNamespaceErr).ToNot(HaveOccurred())

			return nil
		})

		// when
		Expect(featuresHandler.Apply()).To(Succeed())

		// then
		embeddedNs, err := fixtures.GetNamespace(envTestClient, "embedded-test-ns")
		defer objectCleaner.DeleteAll(embeddedNs)
		Expect(err).ToNot(HaveOccurred())
		Expect(embeddedNs.Name).To(Equal("embedded-test-ns"))
	})

	const nsYAML = `apiVersion: v1
kind: Namespace
metadata:
  name: real-file-test-ns`

	It("should source manifests from a specified temporary directory within the file system", func() {
		// given
		tempDir := GinkgoT().TempDir()

		Expect(fixtures.CreateFile(tempDir, "namespace.yaml", nsYAML)).To(Succeed())

		featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
			createServiceErr := feature.CreateFeature("create-namespace").
				For(handler).
				UsingConfig(envTest.Config).
				ManifestSource(os.DirFS(tempDir)).
				Manifests(path.Join("namespace.yaml")). // must be relative to root DirFS defined above
				Load()

			Expect(createServiceErr).ToNot(HaveOccurred())

			return nil
		})

		// when
		Expect(featuresHandler.Apply()).To(Succeed())

		// then
		realNs, err := fixtures.GetNamespace(envTestClient, "real-file-test-ns")
		defer objectCleaner.DeleteAll(realNs)
		Expect(err).ToNot(HaveOccurred())
		Expect(realNs.Name).To(Equal("real-file-test-ns"))
	})

})
