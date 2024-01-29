package features_test

import (
	"context"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
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

	var testFeature *feature.Feature
	var objectCleaner *envtestutil.Cleaner

	BeforeEach(func() {
		c, err := client.New(envTest.Config, client.Options{})
		Expect(err).ToNot(HaveOccurred())

		objectCleaner = envtestutil.CreateCleaner(c, envTest.Config, timeout, interval)

		testFeatureName := "serverless-feature"
		namespace := envtestutil.AppendRandomNameTo(testFeatureName)

		dsciSpec := newDSCInitializationSpec(namespace)
		source := envtestutil.NewSource(featurev1.ComponentType, "kserve")
		testFeature, err = feature.CreateFeature(testFeatureName).
			With(dsciSpec).
			From(source).
			UsingConfig(envTest.Config).
			Load()

		Expect(err).ToNot(HaveOccurred())

		// Creates the actual Feature instance so that associated FeatureTracker is created as well
		Expect(testFeature.Apply()).To(Succeed())
	})

	Context("verifying preconditions", func() {

		When("operator is not installed", func() {
			It("operator presence check should return an error", func() {
				Expect(serverless.EnsureServerlessOperatorInstalled(testFeature)).ToNot(Succeed())
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

			It("operator presence check should succeed", func() {
				Expect(serverless.EnsureServerlessOperatorInstalled(testFeature)).To(Succeed())
			})

			It("KNative serving absence check should succeed if serving is not installed", func() {
				Expect(serverless.EnsureServerlessAbsent(testFeature)).To(Succeed())
			})

			It("KNative serving absence check should fail when serving is present", func() {
				ns := envtestutil.AppendRandomNameTo(testNamespacePrefix)
				nsResource := createNamespace(ns)
				Expect(envTestClient.Create(context.TODO(), nsResource)).To(Succeed())
				defer objectCleaner.DeleteAll(nsResource)

				knativeServing := &unstructured.Unstructured{}
				Expect(yaml.Unmarshal([]byte(knativeServingInstance), knativeServing)).To(Succeed())
				knativeServing.SetNamespace(nsResource.Name)
				Expect(envTestClient.Create(context.TODO(), knativeServing)).To(Succeed())

				Expect(serverless.EnsureServerlessAbsent(testFeature)).ToNot(Succeed())
			})
		})
	})

	Context("default values", func() {

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

		It("should create a TLS secret if certificate is SelfSigned", func() {
			ns := envtestutil.AppendRandomNameTo(testNamespacePrefix)
			nsResource := createNamespace(ns)
			Expect(envTestClient.Create(context.TODO(), nsResource)).To(Succeed())
			defer objectCleaner.DeleteAll(nsResource)

			testFeature.Spec.ControlPlane.Namespace = nsResource.Name
			testFeature.Spec.Serving.IngressGateway.Certificate.Type = infrav1.SelfSigned
			testFeature.Spec.Serving.IngressGateway.Domain = testDomainFooCom
			Expect(serverless.ServingDefaultValues(testFeature)).To(Succeed())
			Expect(serverless.ServingIngressDomain(testFeature)).To(Succeed())

			Expect(serverless.ServingCertificateResource(testFeature)).To(Succeed())

			secret, err := testFeature.Clientset.CoreV1().Secrets(nsResource.Name).Get(context.TODO(), serverless.DefaultCertificateSecretName, v1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(secret).ToNot(BeNil())
		})

		It("should not create any TLS secret if certificate is user provided", func() {
			ns := envtestutil.AppendRandomNameTo(testNamespacePrefix)
			nsResource := createNamespace(ns)
			Expect(envTestClient.Create(context.TODO(), nsResource)).To(Succeed())
			defer objectCleaner.DeleteAll(nsResource)

			testFeature.Spec.ControlPlane.Namespace = nsResource.Name
			testFeature.Spec.Serving.IngressGateway.Certificate.Type = infrav1.Provided
			testFeature.Spec.Serving.IngressGateway.Domain = "*.foo.com"
			Expect(serverless.ServingDefaultValues(testFeature)).To(Succeed())
			Expect(serverless.ServingIngressDomain(testFeature)).To(Succeed())

			Expect(serverless.ServingCertificateResource(testFeature)).To(Succeed())

			list, err := testFeature.Clientset.CoreV1().Secrets(nsResource.Name).List(context.TODO(), v1.ListOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(list.Items).To(BeEmpty())
		})

	})
})
