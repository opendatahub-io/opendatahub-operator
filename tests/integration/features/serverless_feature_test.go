package features_test

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kserve"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/serverless"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	testNamespacePrefix = "test-ns"
	testDomainFooCom    = "*.foo.com"
)

const knativeServingCrd = `apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: knativeservings.operator.knative.dev
spec:
  group: operator.knative.dev
  names:
    kind: KnativeServing
    listKind: KnativeServingList
    plural: knativeservings
    singular: knativeserving
  scope: Namespaced
  versions:
  - name: v1beta1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
`

const knativeServingInstance = `apiVersion: operator.knative.dev/v1beta1
kind: KnativeServing
metadata:
  name: knative-serving-instance
spec: {}
`

const openshiftClusterIngress = `apiVersion: config.openshift.io/v1
kind: Ingress
metadata:
  name: cluster
spec:
  domain: "foo.io"
  loadBalancer:
    platform:
      type: ""`

var _ = Describe("Serverless feature", func() {

	var (
		dsci            *dsciv1.DSCInitialization
		objectCleaner   *envtestutil.Cleaner
		kserveComponent *kserve.Kserve
	)

	BeforeEach(func() {
		// TODO rework
		c, err := client.New(envTest.Config, client.Options{})
		Expect(err).ToNot(HaveOccurred())
		objectCleaner = envtestutil.CreateCleaner(c, envTest.Config, timeout, interval)

		dsci = newDSCInitialization("default")
		kserveComponent = &kserve.Kserve{}
	})

	Context("verifying preconditions", func() {

		When("operator is not installed", func() {

			It("should fail on precondition check", func() {
				// given
				featuresHandler := feature.ComponentFeaturesHandler(kserveComponent, &dsci.Spec, func(handler *feature.FeaturesHandler) error {
					verificationFeatureErr := feature.CreateFeature("no-serverless-operator-check").
						For(handler).
						UsingConfig(envTest.Config).
						PreConditions(serverless.EnsureServerlessOperatorInstalled).
						Load()

					Expect(verificationFeatureErr).ToNot(HaveOccurred())

					return nil
				})

				// when
				applyErr := featuresHandler.Apply()

				// then
				Expect(applyErr).To(MatchError(ContainSubstring("\"knativeservings.operator.knative.dev\" not found")))
			})
		})

		When("operator is installed", func() {

			var knativeServingCrdObj *apiextensionsv1.CustomResourceDefinition

			BeforeEach(func() {
				// Create KNativeServing the CRD
				knativeServingCrdObj = &apiextensionsv1.CustomResourceDefinition{}
				Expect(yaml.Unmarshal([]byte(knativeServingCrd), knativeServingCrdObj)).To(Succeed())
				c, err := client.New(envTest.Config, client.Options{})
				Expect(err).ToNot(HaveOccurred())
				Expect(c.Create(context.TODO(), knativeServingCrdObj)).To(Succeed())

				crdOptions := envtest.CRDInstallOptions{PollInterval: interval, MaxTime: timeout}
				err = envtest.WaitForCRDs(envTest.Config, []*apiextensionsv1.CustomResourceDefinition{knativeServingCrdObj}, crdOptions)
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				// Delete KNativeServing CRD
				objectCleaner.DeleteAll(knativeServingCrdObj)
			})

			It("should succeed checking operator installation using precondition", func() {
				// when
				featuresHandler := feature.ComponentFeaturesHandler(kserveComponent, &dsci.Spec, func(handler *feature.FeaturesHandler) error {
					verificationFeatureErr := feature.CreateFeature("serverless-operator-check").
						For(handler).
						UsingConfig(envTest.Config).
						PreConditions(serverless.EnsureServerlessOperatorInstalled).
						Load()

					Expect(verificationFeatureErr).ToNot(HaveOccurred())

					return nil
				})

				// then
				Expect(featuresHandler.Apply()).To(Succeed())
			})

			It("should succeed if serving is not installed for KNative serving precondition", func() {
				// when
				featuresHandler := feature.ComponentFeaturesHandler(kserveComponent, &dsci.Spec, func(handler *feature.FeaturesHandler) error {
					verificationFeatureErr := feature.CreateFeature("no-serving-installed-yet").
						For(handler).
						UsingConfig(envTest.Config).
						PreConditions(serverless.EnsureServerlessAbsent).
						Load()

					Expect(verificationFeatureErr).ToNot(HaveOccurred())

					return nil
				})

				// then
				Expect(featuresHandler.Apply()).To(Succeed())
			})

			It("should fail if serving is already installed for KNative serving precondition", func() {
				// given
				ns := envtestutil.AppendRandomNameTo(testNamespacePrefix)
				nsResource := newNamespace(ns)
				Expect(envTestClient.Create(context.TODO(), nsResource)).To(Succeed())
				defer objectCleaner.DeleteAll(nsResource)

				knativeServing := &unstructured.Unstructured{}
				Expect(yaml.Unmarshal([]byte(knativeServingInstance), knativeServing)).To(Succeed())
				knativeServing.SetNamespace(nsResource.Name)
				Expect(envTestClient.Create(context.TODO(), knativeServing)).To(Succeed())

				// when
				featuresHandler := feature.ComponentFeaturesHandler(kserveComponent, &dsci.Spec, func(handler *feature.FeaturesHandler) error {
					verificationFeatureErr := feature.CreateFeature("serving-already-installed").
						For(handler).
						UsingConfig(envTest.Config).
						PreConditions(serverless.EnsureServerlessAbsent).
						Load()

					Expect(verificationFeatureErr).ToNot(HaveOccurred())

					return nil
				})

				// then
				Expect(featuresHandler.Apply()).ToNot(Succeed())
			})
		})

	})

	Context("default values", func() {

		var testFeature *feature.Feature

		BeforeEach(func() {
			// Stubbing feature as we want to test particular functions in isolation
			testFeature = &feature.Feature{
				Name: "test-feature",
				Spec: &feature.Spec{
					ServiceMeshSpec: &infrav1.ServiceMeshSpec{},
					Serving:         &infrav1.ServingSpec{},
				},
			}

			testFeature.Client = envTestClient
			testFeature.Clientset = envTestClientset
			var err error
			testFeature.DynamicClient, err = dynamic.NewForConfig(envTest.Config)
			Expect(err).ToNot(HaveOccurred())
		})

		Context("ingress gateway TLS secret name", func() {

			It("should set default value when value is empty in the DSCI", func() {
				// Default value is blank -> testFeature.Spec.Serving.IngressGateway.Certificate.SecretName = ""
				Expect(serverless.ServingDefaultValues(testFeature)).To(Succeed())
				Expect(testFeature.Spec.KnativeCertificateSecret).To(Equal(serverless.DefaultCertificateSecretName))
			})

			It("should use user value when set in the DSCI", func() {
				testFeature.Spec.Serving.IngressGateway.Certificate.SecretName = "fooBar"
				Expect(serverless.ServingDefaultValues(testFeature)).To(Succeed())
				Expect(testFeature.Spec.KnativeCertificateSecret).To(Equal("fooBar"))
			})
		})

		Context("ingress domain name suffix", func() {

			It("should use OpenShift ingress domain when value is empty in the DSCI", func() {
				// Create KNativeServing the CRD
				osIngressResource := &unstructured.Unstructured{}
				Expect(yaml.Unmarshal([]byte(openshiftClusterIngress), osIngressResource)).ToNot(HaveOccurred())
				c, err := client.New(envTest.Config, client.Options{})
				Expect(err).ToNot(HaveOccurred())
				Expect(c.Create(context.TODO(), osIngressResource)).To(Succeed())

				// Default value is blank -> testFeature.Spec.Serving.IngressGateway.Domain = ""
				Expect(serverless.ServingIngressDomain(testFeature)).To(Succeed())
				Expect(testFeature.Spec.KnativeIngressDomain).To(Equal("*.foo.io"))
			})

			It("should use user value when set in the DSCI", func() {
				testFeature.Spec.Serving.IngressGateway.Domain = testDomainFooCom
				Expect(serverless.ServingIngressDomain(testFeature)).To(Succeed())
				Expect(testFeature.Spec.KnativeIngressDomain).To(Equal(testDomainFooCom))
			})
		})
	})

	Context("resources creation", func() {

		var (
			namespace *corev1.Namespace
		)

		BeforeEach(func() {
			ns := envtestutil.AppendRandomNameTo(testNamespacePrefix)
			namespace = newNamespace(ns)
			Expect(envTestClient.Create(context.TODO(), namespace)).To(Succeed())

			dsci.Spec.ServiceMesh.ControlPlane.Namespace = ns
		})

		AfterEach(func() {
			objectCleaner.DeleteAll(namespace)
		})

		It("should create a TLS secret if certificate is SelfSigned", func() {
			// given
			kserveComponent.Serving.IngressGateway.Certificate.Type = infrav1.SelfSigned
			kserveComponent.Serving.IngressGateway.Domain = testDomainFooCom

			featuresHandler := feature.ComponentFeaturesHandler(kserveComponent, &dsci.Spec, func(handler *feature.FeaturesHandler) error {
				verificationFeatureErr := feature.CreateFeature("tls-secret-creation").
					For(handler).
					UsingConfig(envTest.Config).
					WithData(
						kserve.PopulateComponentSettings(kserveComponent),
						serverless.ServingDefaultValues,
						serverless.ServingIngressDomain,
					).
					WithResources(serverless.ServingCertificateResource).
					Load()

				Expect(verificationFeatureErr).ToNot(HaveOccurred())

				return nil
			})

			// when
			Expect(featuresHandler.Apply()).To(Succeed())

			// then
			Eventually(func() error {
				secret, err := envTestClientset.CoreV1().Secrets(namespace.Name).Get(context.TODO(), serverless.DefaultCertificateSecretName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if secret == nil {
					return fmt.Errorf("secret not found")
				}

				return nil
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
		})

		It("should not create any TLS secret if certificate is user provided", func() {
			// given
			kserveComponent.Serving.IngressGateway.Certificate.Type = infrav1.Provided
			kserveComponent.Serving.IngressGateway.Domain = testDomainFooCom
			featuresHandler := feature.ComponentFeaturesHandler(kserveComponent, &dsci.Spec, func(handler *feature.FeaturesHandler) error {
				verificationFeatureErr := feature.CreateFeature("tls-secret-creation").
					For(handler).
					UsingConfig(envTest.Config).
					WithData(
						kserve.PopulateComponentSettings(kserveComponent),
						serverless.ServingDefaultValues,
						serverless.ServingIngressDomain,
					).
					WithResources(serverless.ServingCertificateResource).
					Load()

				Expect(verificationFeatureErr).ToNot(HaveOccurred())

				return nil
			})

			// when
			Expect(featuresHandler.Apply()).To(Succeed())

			// then
			Consistently(func() error {
				list, err := envTestClientset.CoreV1().Secrets(namespace.Name).List(context.TODO(), metav1.ListOptions{})
				if err != nil || len(list.Items) != 0 {
					return fmt.Errorf("list len: %d, error: %w", len(list.Items), err)
				}

				return nil
			}).WithTimeout(timeout).WithPolling(interval).Should(Succeed())
		})

	})

})
