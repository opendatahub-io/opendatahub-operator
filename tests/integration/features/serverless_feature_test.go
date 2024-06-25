package features_test

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/serverless"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/integration/features/fixtures"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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
		objectCleaner = envtestutil.CreateCleaner(c, envTest.Config, fixtures.Timeout, fixtures.Interval)

		dsci = fixtures.NewDSCInitialization("default")
		kserveComponent = &kserve.Kserve{}
	})

	Context("verifying preconditions", func() {

		When("operator is not installed", func() {

			It("should fail on precondition check", func(ctx context.Context) {
				// given
				featuresHandler := feature.ComponentFeaturesHandler(kserveComponent.GetComponentName(), &dsci.Spec, func(handler *feature.FeaturesHandler) error {
					verificationFeatureErr := feature.CreateFeature("no-serverless-operator-check").
						For(handler).
						UsingConfig(envTest.Config).
						PreConditions(serverless.EnsureServerlessOperatorInstalled).
						Load()

					Expect(verificationFeatureErr).ToNot(HaveOccurred())

					return nil
				})

				// when
				applyErr := featuresHandler.Apply(ctx)

				// then
				Expect(applyErr).To(MatchError(ContainSubstring("failed to find the pre-requisite operator subscription \"serverless-operator\"")))
			})
		})

		When("operator is installed", func() {

			var knativeServingCrdObj *apiextensionsv1.CustomResourceDefinition

			BeforeEach(func(ctx context.Context) {
				err := fixtures.CreateSubscription(ctx, envTestClient, "openshift-serverless", fixtures.KnativeServingSubscription)
				Expect(err).ToNot(HaveOccurred())

				// Create KNativeServing the CRD
				knativeServingCrdObj = &apiextensionsv1.CustomResourceDefinition{}
				Expect(yaml.Unmarshal([]byte(fixtures.KnativeServingCrd), knativeServingCrdObj)).To(Succeed())
				c, err := client.New(envTest.Config, client.Options{})
				Expect(err).ToNot(HaveOccurred())
				Expect(c.Create(ctx, knativeServingCrdObj)).To(Succeed())

				crdOptions := envtest.CRDInstallOptions{PollInterval: fixtures.Interval, MaxTime: fixtures.Timeout}
				err = envtest.WaitForCRDs(envTest.Config, []*apiextensionsv1.CustomResourceDefinition{knativeServingCrdObj}, crdOptions)
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func(ctx context.Context) {
				// Delete KNativeServing CRD
				objectCleaner.DeleteAll(ctx, knativeServingCrdObj)
			})

			It("should succeed checking operator installation using precondition", func(ctx context.Context) {
				// when
				featuresHandler := feature.ComponentFeaturesHandler(kserveComponent.GetComponentName(), &dsci.Spec, func(handler *feature.FeaturesHandler) error {
					verificationFeatureErr := feature.CreateFeature("serverless-operator-check").
						For(handler).
						UsingConfig(envTest.Config).
						PreConditions(serverless.EnsureServerlessOperatorInstalled).
						Load()

					Expect(verificationFeatureErr).ToNot(HaveOccurred())

					return nil
				})

				// then
				Expect(featuresHandler.Apply(ctx)).To(Succeed())
			})

			It("should succeed if serving is not installed for KNative serving precondition", func(ctx context.Context) {
				// when
				featuresHandler := feature.ComponentFeaturesHandler(kserveComponent.GetComponentName(), &dsci.Spec, func(handler *feature.FeaturesHandler) error {
					verificationFeatureErr := feature.CreateFeature("no-serving-installed-yet").
						For(handler).
						UsingConfig(envTest.Config).
						PreConditions(serverless.EnsureServerlessAbsent).
						Load()

					Expect(verificationFeatureErr).ToNot(HaveOccurred())

					return nil
				})

				// then
				Expect(featuresHandler.Apply(ctx)).To(Succeed())
			})

			It("should fail if serving is already installed for KNative serving precondition", func(ctx context.Context) {
				// given
				ns := envtestutil.AppendRandomNameTo(fixtures.TestNamespacePrefix)
				nsResource := fixtures.NewNamespace(ns)
				Expect(envTestClient.Create(ctx, nsResource)).To(Succeed())
				defer objectCleaner.DeleteAll(ctx, nsResource)

				knativeServing := &unstructured.Unstructured{}
				Expect(yaml.Unmarshal([]byte(fixtures.KnativeServingInstance), knativeServing)).To(Succeed())
				knativeServing.SetNamespace(nsResource.Name)
				Expect(envTestClient.Create(ctx, knativeServing)).To(Succeed())

				// when
				featuresHandler := feature.ComponentFeaturesHandler(kserveComponent.GetComponentName(), &dsci.Spec, func(handler *feature.FeaturesHandler) error {
					verificationFeatureErr := feature.CreateFeature("serving-already-installed").
						For(handler).
						UsingConfig(envTest.Config).
						PreConditions(serverless.EnsureServerlessAbsent).
						Load()

					Expect(verificationFeatureErr).ToNot(HaveOccurred())

					return nil
				})

				// then
				Expect(featuresHandler.Apply(ctx)).ToNot(Succeed())
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
		})

		Context("ingress gateway TLS secret name", func() {

			It("should set default value when value is empty in the DSCI", func(ctx context.Context) {
				// Default value is blank -> testFeature.Spec.Serving.IngressGateway.Certificate.SecretName = ""
				Expect(serverless.ServingDefaultValues(ctx, testFeature)).To(Succeed())
				Expect(testFeature.Spec.KnativeCertificateSecret).To(Equal(serverless.DefaultCertificateSecretName))
			})

			It("should use user value when set in the DSCI", func(ctx context.Context) {
				testFeature.Spec.Serving.IngressGateway.Certificate.SecretName = "fooBar"
				Expect(serverless.ServingDefaultValues(ctx, testFeature)).To(Succeed())
				Expect(testFeature.Spec.KnativeCertificateSecret).To(Equal("fooBar"))
			})
		})

		Context("ingress domain name suffix", func() {

			It("should use OpenShift ingress domain when value is empty in the DSCI", func(ctx context.Context) {
				// Create KNativeServing the CRD
				osIngressResource := &unstructured.Unstructured{}
				Expect(yaml.Unmarshal([]byte(fixtures.OpenshiftClusterIngress), osIngressResource)).ToNot(HaveOccurred())
				c, err := client.New(envTest.Config, client.Options{})
				Expect(err).ToNot(HaveOccurred())
				Expect(c.Create(ctx, osIngressResource)).To(Succeed())

				// Default value is blank -> testFeature.Spec.Serving.IngressGateway.Domain = ""
				Expect(serverless.ServingIngressDomain(ctx, testFeature)).To(Succeed())
				Expect(testFeature.Spec.KnativeIngressDomain).To(Equal("*.foo.io"))
			})

			It("should use user value when set in the DSCI", func(ctx context.Context) {
				testFeature.Spec.Serving.IngressGateway.Domain = fixtures.TestDomainFooCom
				Expect(serverless.ServingIngressDomain(ctx, testFeature)).To(Succeed())
				Expect(testFeature.Spec.KnativeIngressDomain).To(Equal(fixtures.TestDomainFooCom))
			})
		})
	})

	Context("resources creation", func() {

		var (
			namespace *corev1.Namespace
		)

		BeforeEach(func(ctx context.Context) {
			ns := envtestutil.AppendRandomNameTo(fixtures.TestNamespacePrefix)
			namespace = fixtures.NewNamespace(ns)
			Expect(envTestClient.Create(ctx, namespace)).To(Succeed())

			dsci.Spec.ServiceMesh.ControlPlane.Namespace = ns
		})

		AfterEach(func(ctx context.Context) {
			objectCleaner.DeleteAll(ctx, namespace)
		})

		It("should create a TLS secret if certificate is SelfSigned", func(ctx context.Context) {
			// given
			kserveComponent.Serving.IngressGateway.Certificate.Type = infrav1.SelfSigned
			kserveComponent.Serving.IngressGateway.Domain = fixtures.TestDomainFooCom

			featuresHandler := feature.ComponentFeaturesHandler(kserveComponent.GetComponentName(), &dsci.Spec, func(handler *feature.FeaturesHandler) error {
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
			Expect(featuresHandler.Apply(ctx)).To(Succeed())

			// then
			Eventually(func() error {
				secret, err := envTestClientset.CoreV1().Secrets(namespace.Name).Get(ctx, serverless.DefaultCertificateSecretName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if secret == nil {
					return errors.New("secret not found")
				}

				return nil
			}).WithTimeout(fixtures.Timeout).WithPolling(fixtures.Interval).Should(Succeed())
		})

		It("should not create any TLS secret if certificate is user provided", func(ctx context.Context) {
			// given
			kserveComponent.Serving.IngressGateway.Certificate.Type = infrav1.Provided
			kserveComponent.Serving.IngressGateway.Domain = fixtures.TestDomainFooCom
			featuresHandler := feature.ComponentFeaturesHandler(kserveComponent.GetComponentName(), &dsci.Spec, func(handler *feature.FeaturesHandler) error {
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
			Expect(featuresHandler.Apply(ctx)).To(Succeed())

			// then
			Consistently(func() error {
				list, err := envTestClientset.CoreV1().Secrets(namespace.Name).List(ctx, metav1.ListOptions{})
				if err != nil || len(list.Items) != 0 {
					return fmt.Errorf("list len: %d, error: %w", len(list.Items), err)
				}

				return nil
			}).WithTimeout(fixtures.Timeout).WithPolling(fixtures.Interval).Should(Succeed())
		})

	})

})
