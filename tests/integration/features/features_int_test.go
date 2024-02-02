package features_test

import (
	"context"
	"embed"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
)

//go:embed templates
var testEmbeddedFiles embed.FS

const (
	timeout  = 5 * time.Second
	interval = 250 * time.Millisecond
)

var _ = Describe("feature preconditions", func() {

	Context("namespace existence", func() {

		var (
			objectCleaner *envtestutil.Cleaner
			namespace     string
			dsci          *dsciv1.DSCInitialization
		)

		BeforeEach(func() {
			objectCleaner = envtestutil.CreateCleaner(envTestClient, envTest.Config, timeout, interval)

			testFeatureName := "test-ns-creation"
			namespace = envtestutil.AppendRandomNameTo(testFeatureName)
			dsci = newDSCInitialization(namespace)
		})

		It("should create namespace if it does not exist", func() {
			// given
			_, err := getNamespace(namespace)
			Expect(errors.IsNotFound(err)).To(BeTrue())
			defer objectCleaner.DeleteAll(&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})

			// when
			featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
				testFeatureErr := feature.CreateFeature("create-new-ns").
					For(handler).
					PreConditions(feature.CreateNamespaceIfNotExists(namespace)).
					UsingConfig(envTest.Config).
					Load()

				Expect(testFeatureErr).ToNot(HaveOccurred())

				return nil
			})

			// then
			Expect(featuresHandler.Apply()).To(Succeed())

			// and
			Eventually(func() error {
				_, err := getNamespace(namespace)
				return err
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
		})

		It("should not try to create namespace if it does already exist", func() {
			// given
			ns := newNamespace(namespace)
			Expect(envTestClient.Create(context.Background(), ns)).To(Succeed())
			Eventually(func() error {
				_, err := getNamespace(namespace)
				return err
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed()) // wait for ns to actually get created

			defer objectCleaner.DeleteAll(ns)

			// when
			featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
				testFeatureErr := feature.CreateFeature("create-new-ns").
					For(handler).
					PreConditions(feature.CreateNamespaceIfNotExists(namespace)).
					UsingConfig(envTest.Config).
					Load()

				Expect(testFeatureErr).ToNot(HaveOccurred())

				return nil
			})

			// then
			Expect(featuresHandler.Apply()).To(Succeed())

		})

	})

	Context("ensuring custom resource definitions are installed", func() {

		var (
			dsci *dsciv1.DSCInitialization
		)

		BeforeEach(func() {
			namespace := envtestutil.AppendRandomNameTo("test-crd-creation")
			dsci = newDSCInitialization(namespace)
		})

		It("should successfully check for existing CRD", func() {
			// given example CRD installed into env
			name := "test-resources.openshift.io"

			// when
			featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
				crdVerificationErr := feature.CreateFeature("verify-crd-exists").
					For(handler).
					UsingConfig(envTest.Config).
					PreConditions(feature.EnsureCRDIsInstalled(name)).
					Load()

				Expect(crdVerificationErr).ToNot(HaveOccurred())

				return nil
			})

			// then
			Expect(featuresHandler.Apply()).To(Succeed())
		})

		It("should fail to check non-existing CRD", func() {
			// given
			name := "non-existing-resource.non-existing-group.io"

			// when
			featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
				crdVerificationErr := feature.CreateFeature("fail-on-non-existing-crd").
					For(handler).
					UsingConfig(envTest.Config).
					PreConditions(feature.EnsureCRDIsInstalled(name)).
					Load()

				Expect(crdVerificationErr).ToNot(HaveOccurred())

				return nil
			})

			// then
			Expect(featuresHandler.Apply()).To(MatchError(ContainSubstring("\"non-existing-resource.non-existing-group.io\" not found")))
		})
	})
})

var _ = Describe("feature cleanup", func() {

	Context("using FeatureTracker and ownership as cleanup strategy", Ordered, func() {

		const (
			featureName = "create-secret"
			secretName  = "test-secret"
		)

		var (
			dsci            *dsciv1.DSCInitialization
			namespace       string
			featuresHandler *feature.FeaturesHandler
		)

		BeforeAll(func() {
			namespace = envtestutil.AppendRandomNameTo("test-secret-ownership")
			dsci = newDSCInitialization(namespace)
			featuresHandler = feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
				secretCreationErr := feature.CreateFeature(featureName).
					For(handler).
					UsingConfig(envTest.Config).
					PreConditions(
						feature.CreateNamespaceIfNotExists(namespace),
					).
					WithResources(createSecret(secretName, namespace)).
					Load()

				Expect(secretCreationErr).ToNot(HaveOccurred())

				return nil
			})

		})

		It("should successfully create resource and associated feature tracker", func() {
			// when
			Expect(featuresHandler.Apply()).Should(Succeed())

			// then
			Eventually(createdSecretHasOwnerReferenceToOwningFeature(secretName, namespace)).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(Succeed())
		})

		It("should remove feature tracker on clean-up", func() {
			// when
			Expect(featuresHandler.Delete()).To(Succeed())

			// then
			Eventually(createdSecretHasOwnerReferenceToOwningFeature(secretName, namespace)).
				WithTimeout(timeout).
				WithPolling(interval).
				Should(WithTransform(errors.IsNotFound, BeTrue()))
		})

	})

	var _ = Describe("feature trackers", func() {
		Context("ensuring feature trackers indicate status and phase", func() {

			const appNamespace = "default"

			var (
				dsci *dsciv1.DSCInitialization
			)

			BeforeEach(func() {
				dsci = newDSCInitialization("default")
			})

			It("should indicate successful installation in FeatureTracker", func() {
				// given example CRD installed into env
				name := "test-resources.openshift.io"
				featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
					verificationFeatureErr := feature.CreateFeature("crd-verification").
						For(handler).
						UsingConfig(envTest.Config).
						PreConditions(feature.EnsureCRDIsInstalled(name)).
						Load()

					Expect(verificationFeatureErr).ToNot(HaveOccurred())

					return nil
				})

				// when
				Expect(featuresHandler.Apply()).To(Succeed())

				// then
				featureTracker, err := getFeatureTracker("crd-verification", appNamespace)
				Expect(err).ToNot(HaveOccurred())
				Expect(*featureTracker.Status.Conditions).To(ContainElement(
					MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(conditionsv1.ConditionAvailable),
						"Status": Equal(v1.ConditionTrue),
						"Reason": Equal(string(featurev1.FeatureCreated)),
					}),
				))
			})

			It("should indicate failure in preconditions", func() {
				// given
				name := "non-existing-resource.non-existing-group.io"
				featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
					verificationFeatureErr := feature.CreateFeature("non-existing-crd-verification").
						For(handler).
						UsingConfig(envTest.Config).
						PreConditions(feature.EnsureCRDIsInstalled(name)).
						Load()

					Expect(verificationFeatureErr).ToNot(HaveOccurred())

					return nil
				})

				// when
				Expect(featuresHandler.Apply()).ToNot(Succeed())

				// then
				featureTracker, err := getFeatureTracker("non-existing-crd-verification", appNamespace)
				Expect(err).ToNot(HaveOccurred())
				Expect(*featureTracker.Status.Conditions).To(ContainElement(
					MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(conditionsv1.ConditionDegraded),
						"Status": Equal(v1.ConditionTrue),
						"Reason": Equal(string(featurev1.PreConditions)),
					}),
				))
			})

			It("should indicate failure in post-conditions", func() {
				// given
				featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
					verificationFeatureErr := feature.CreateFeature("post-condition-failure").
						For(handler).
						UsingConfig(envTest.Config).
						PostConditions(func(f *feature.Feature) error {
							return fmt.Errorf("during test always fail")
						}).
						Load()

					Expect(verificationFeatureErr).ToNot(HaveOccurred())

					return nil
				})

				// when
				Expect(featuresHandler.Apply()).ToNot(Succeed())

				// then
				featureTracker, err := getFeatureTracker("post-condition-failure", appNamespace)
				Expect(err).ToNot(HaveOccurred())
				Expect(*featureTracker.Status.Conditions).To(ContainElement(
					MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(conditionsv1.ConditionDegraded),
						"Status": Equal(v1.ConditionTrue),
						"Reason": Equal(string(featurev1.PostConditions)),
					}),
				))
			})

			It("should correctly indicate source in the feature tracker", func() {
				// given
				featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
					emptyFeatureErr := feature.CreateFeature("empty-feature").
						For(handler).
						UsingConfig(envTest.Config).
						Load()

					Expect(emptyFeatureErr).ToNot(HaveOccurred())

					return nil
				})

				// when
				Expect(featuresHandler.Apply()).To(Succeed())

				// then
				featureTracker, err := getFeatureTracker("empty-feature", appNamespace)
				Expect(err).ToNot(HaveOccurred())
				Expect(featureTracker.Spec.Source).To(
					MatchFields(IgnoreExtras, Fields{
						"Name": Equal("default-dsci"),
						"Type": Equal(featurev1.DSCIType),
					}),
				)
			})

			It("should correctly indicate app namespace in the feature tracker", func() {
				// given
				featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
					emptyFeatureErr := feature.CreateFeature("empty-feature").
						For(handler).
						UsingConfig(envTest.Config).
						Load()

					Expect(emptyFeatureErr).ToNot(HaveOccurred())

					return nil
				})

				// when
				Expect(featuresHandler.Apply()).To(Succeed())

				// then
				featureTracker, err := getFeatureTracker("empty-feature", appNamespace)
				Expect(err).ToNot(HaveOccurred())
				Expect(featureTracker.Spec.AppNamespace).To(Equal("default"))
			})

		})

	})

	var _ = Describe("Manifest sources", func() {
		Context("using various manifest sources", func() {

			var (
				objectCleaner *envtestutil.Cleaner
				dsci          *dsciv1.DSCInitialization
				namespace     *v1.Namespace
			)

			BeforeEach(func() {
				objectCleaner = envtestutil.CreateCleaner(envTestClient, envTest.Config, timeout, interval)
				nsName := envtestutil.AppendRandomNameTo("smcp-ns")

				var err error
				namespace, err = cluster.CreateNamespace(envTestClient, nsName)
				Expect(err).ToNot(HaveOccurred())

				dsci = newDSCInitialization(nsName)
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
				service, err := getService("knative-local-gateway", namespace.Name)
				Expect(err).ToNot(HaveOccurred())
				Expect(service.Name).To(Equal("knative-local-gateway"))
			})

			It("should be able to process an embedded YAML file from the default location", func() {
				// given
				knativeNs, nsErr := cluster.CreateNamespace(envTestClient, "knative-serving")
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
						ManifestSource(testEmbeddedFiles).
						Manifests(path.Join(feature.BaseDir, "namespace.yaml")).
						Load()

					Expect(createNamespaceErr).ToNot(HaveOccurred())

					return nil
				})

				// when
				Expect(featuresHandler.Apply()).To(Succeed())

				// then
				embeddedNs, err := getNamespace("embedded-test-ns")
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

				Expect(createFile(tempDir, "namespace.yaml", nsYAML)).To(Succeed())

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
				realNs, err := getNamespace("real-file-test-ns")
				defer objectCleaner.DeleteAll(realNs)
				Expect(err).ToNot(HaveOccurred())
				Expect(realNs.Name).To(Equal("real-file-test-ns"))
			})
		})
	})

})

func createdSecretHasOwnerReferenceToOwningFeature(secretName, namespace string) func() error {
	return func() error {
		secret, err := envTestClientset.CoreV1().
			Secrets(namespace).
			Get(context.TODO(), secretName, metav1.GetOptions{})

		if err != nil {
			return err
		}

		Expect(secret.OwnerReferences).Should(
			ContainElement(
				MatchFields(IgnoreExtras, Fields{"Kind": Equal("FeatureTracker")}),
			),
		)

		trackerName := ""
		for _, ownerRef := range secret.OwnerReferences {
			if ownerRef.Kind == "FeatureTracker" {
				trackerName = ownerRef.Name
				break
			}
		}

		tracker := &featurev1.FeatureTracker{}
		return envTestClient.Get(context.Background(), client.ObjectKey{
			Name: trackerName,
		}, tracker)
	}
}

func createSecret(name, namespace string) func(f *feature.Feature) error {
	return func(f *feature.Feature) error {
		secret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{
					f.AsOwnerReference(),
				},
			},
			Data: map[string][]byte{
				"test": []byte("test"),
			},
		}

		_, err := f.Clientset.CoreV1().
			Secrets(namespace).
			Create(context.TODO(), secret, metav1.CreateOptions{})

		return err
	}
}

func newNamespace(name string) *v1.Namespace {
	return &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
}

func getFeatureTracker(featureName, appNamespace string) (*featurev1.FeatureTracker, error) { //nolint:unparam //reason appNs
	tracker := featurev1.NewFeatureTracker(featureName, appNamespace)
	err := envTestClient.Get(context.Background(), client.ObjectKey{
		Name: tracker.Name,
	}, tracker)

	return tracker, err
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

func getNamespace(namespace string) (*v1.Namespace, error) {
	ns := newNamespace(namespace)
	err := envTestClient.Get(context.Background(), types.NamespacedName{Name: namespace}, ns)

	return ns, err
}

func getService(name, namespace string) (*v1.Service, error) {
	svc := &v1.Service{}
	err := envTestClient.Get(context.Background(), types.NamespacedName{
		Name: name, Namespace: namespace,
	}, svc)

	return svc, err
}

func createFile(dir, filename, data string) error {
	filePath := filepath.Join(dir, filename)
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}

	_, err = file.WriteString(data)
	if err != nil {
		return err
	}
	return file.Sync()
}
