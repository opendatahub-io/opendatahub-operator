package features_test

import (
	"context"
	"os"
	"path"

	corev1 "k8s.io/api/core/v1"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/provider"
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

	BeforeEach(func(ctx context.Context) {
		objectCleaner = envtestutil.CreateCleaner(envTestClient, envTest.Config, fixtures.Timeout, fixtures.Interval)
		nsName := envtestutil.AppendRandomNameTo("smcp-ns")

		var err error
		namespace, err = cluster.CreateNamespace(ctx, envTestClient, nsName)
		Expect(err).ToNot(HaveOccurred())

		dsci = fixtures.NewDSCInitialization(nsName)
		dsci.Spec.ServiceMesh.ControlPlane.Namespace = namespace.Name
	})

	AfterEach(func(ctx context.Context) {
		objectCleaner.DeleteAll(ctx, namespace)
	})

	It("should be able to process an embedded YAML file", func(ctx context.Context) {
		// given
		featuresHandler := feature.ClusterFeaturesHandler(dsci, func(registry feature.FeaturesRegistry) error {
			errNsCreate := registry.Add(feature.Define("create-namespace").
				UsingConfig(envTest.Config).
				ManifestsLocation(fixtures.TestEmbeddedFiles).
				Manifests(path.Join(fixtures.BaseDir, "namespace.yaml")),
			)

			Expect(errNsCreate).ToNot(HaveOccurred())

			return nil
		})

		// when
		Expect(featuresHandler.Apply(ctx)).To(Succeed())

		// then
		embeddedNs, err := fixtures.GetNamespace(ctx, envTestClient, "embedded-test-ns")
		defer objectCleaner.DeleteAll(ctx, embeddedNs)
		Expect(err).ToNot(HaveOccurred())
		Expect(embeddedNs.Name).To(Equal("embedded-test-ns"))
	})

	It("should be able to process an embedded template file", func(ctx context.Context) {
		// given
		featuresHandler := feature.ClusterFeaturesHandler(dsci, func(registry feature.FeaturesRegistry) error {
			errSvcCreate := registry.Add(feature.Define("create-local-gw-svc").
				UsingConfig(envTest.Config).
				ManifestsLocation(fixtures.TestEmbeddedFiles).
				Manifests(path.Join(fixtures.BaseDir, "local-gateway-svc.tmpl.yaml")).
				WithData(feature.Entry("ControlPlane", provider.ValueOf(dsci.Spec.ServiceMesh.ControlPlane).Get)),
			)

			Expect(errSvcCreate).ToNot(HaveOccurred())

			return nil
		})

		// when
		Expect(featuresHandler.Apply(ctx)).To(Succeed())

		// then
		service, err := fixtures.GetService(ctx, envTestClient, namespace.Name, "knative-local-gateway")
		Expect(err).ToNot(HaveOccurred())
		Expect(service.Name).To(Equal("knative-local-gateway"))
	})

	const nsYAML = `apiVersion: v1
kind: Namespace
metadata:
  name: real-file-test-ns`

	It("should source manifests from a specified temporary directory within the file system", func(ctx context.Context) {
		// given
		tempDir := GinkgoT().TempDir()

		Expect(fixtures.CreateFile(tempDir, "namespace.yaml", nsYAML)).To(Succeed())

		featuresHandler := feature.ClusterFeaturesHandler(dsci, func(registry feature.FeaturesRegistry) error {
			errSvcCreate := registry.Add(feature.Define("create-namespace").
				UsingConfig(envTest.Config).
				ManifestsLocation(os.DirFS(tempDir)).
				Manifests(path.Join("namespace.yaml")), // must be relative to root DirFS defined above
			)

			Expect(errSvcCreate).ToNot(HaveOccurred())

			return nil
		})

		// when
		Expect(featuresHandler.Apply(ctx)).To(Succeed())

		// then
		realNs, err := fixtures.GetNamespace(ctx, envTestClient, "real-file-test-ns")
		defer objectCleaner.DeleteAll(ctx, realNs)
		Expect(err).ToNot(HaveOccurred())
		Expect(realNs.Name).To(Equal("real-file-test-ns"))
	})
})
