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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
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

	BeforeEach(func(ctx context.Context) {
		// TODO rework
		c, err := client.New(envTest.Config, client.Options{})
		Expect(err).ToNot(HaveOccurred())
		objectCleaner = envtestutil.CreateCleaner(c, envTest.Config, fixtures.Timeout, fixtures.Interval)

		namespace := envtestutil.AppendRandomNameTo("ns-serverless")
		dsciName := envtestutil.AppendRandomNameTo("dsci-serverless")
		dsci = fixtures.NewDSCInitialization(ctx, envTestClient, dsciName, namespace)
		kserveComponent = &kserve.Kserve{}
	})

	Context("verifying preconditions", func() {

		When("operator is not installed", func() {

			It("should fail on precondition check", func(ctx context.Context) {
				// given
				featuresProvider := func(registry feature.FeaturesRegistry) error {
					errFeatureAdd := registry.Add(
						feature.Define("no-serverless-operator-check").
							PreConditions(serverless.EnsureServerlessOperatorInstalled),
					)

					Expect(errFeatureAdd).ToNot(HaveOccurred())

					return nil
				}

				featuresHandler := feature.ComponentFeaturesHandler(dsci, kserveComponent.GetComponentName(), dsci.Spec.ApplicationsNamespace, featuresProvider)

				// when
				applyErr := featuresHandler.Apply(ctx, envTestClient)

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
				featuresProvider := func(registry feature.FeaturesRegistry) error {
					errFeatureAdd := registry.Add(
						feature.Define("serverless-operator-check").
							PreConditions(serverless.EnsureServerlessOperatorInstalled),
					)

					Expect(errFeatureAdd).ToNot(HaveOccurred())

					return nil
				}

				featuresHandler := feature.ComponentFeaturesHandler(dsci, kserveComponent.GetComponentName(), dsci.Spec.ApplicationsNamespace, featuresProvider)

				// then
				Expect(featuresHandler.Apply(ctx, envTestClient)).To(Succeed())
			})

			It("should succeed if serving is not installed for KNative serving precondition", func(ctx context.Context) {
				// when
				featuresProvider := func(registry feature.FeaturesRegistry) error {
					errFeatureAdd := registry.Add(
						feature.Define("no-serving-installed-yet").
							PreConditions(serverless.EnsureServerlessAbsent),
					)

					Expect(errFeatureAdd).ToNot(HaveOccurred())

					return nil
				}

				featuresHandler := feature.ComponentFeaturesHandler(dsci, kserveComponent.GetComponentName(), dsci.Spec.ApplicationsNamespace, featuresProvider)

				// then
				Expect(featuresHandler.Apply(ctx, envTestClient)).To(Succeed())
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
				featuresProvider := func(registry feature.FeaturesRegistry) error {
					errFeatureAdd := registry.Add(
						feature.Define("serving-already-installed").
							PreConditions(serverless.EnsureServerlessAbsent),
					)

					Expect(errFeatureAdd).ToNot(HaveOccurred())

					return nil
				}

				featuresHandler := feature.ComponentFeaturesHandler(dsci, kserveComponent.GetComponentName(), dsci.Spec.ApplicationsNamespace, featuresProvider)

				// then
				Expect(featuresHandler.Apply(ctx, envTestClient)).ToNot(Succeed())
			})
		})

	})

	Context("default values", func() {

		Context("ingress gateway TLS secret name", func() {

			It("should set default value when value is empty in the DSCI", func(ctx context.Context) {
				// given
				serving := infrav1.ServingSpec{
					IngressGateway: infrav1.GatewaySpec{
						Certificate: infrav1.CertificateSpec{
							SecretName: "",
						},
					},
				}

				// when
				actualSecretName, err := serverless.FeatureData.CertificateName.Define(&serving).Value(ctx, envTestClient)

				// then
				Expect(err).ToNot(HaveOccurred())
				Expect(actualSecretName).To(Equal(serverless.DefaultCertificateSecretName))
			})

			It("should use user value when set in the DSCI", func(ctx context.Context) {
				// given
				serving := infrav1.ServingSpec{
					IngressGateway: infrav1.GatewaySpec{
						Certificate: infrav1.CertificateSpec{
							SecretName: "top-secret-service",
						},
					},
				}

				// when
				actualSecretName, err := serverless.FeatureData.CertificateName.Define(&serving).Value(ctx, envTestClient)

				// then
				Expect(err).ToNot(HaveOccurred())
				Expect(actualSecretName).To(Equal("top-secret-service"))
			})
		})

		Context("ingress domain name suffix", func() {

			It("should use OpenShift ingress domain when value is empty in the DSCI", func(ctx context.Context) {
				// given
				osIngressResource := &unstructured.Unstructured{}
				Expect(yaml.Unmarshal([]byte(fixtures.OpenshiftClusterIngress), osIngressResource)).ToNot(HaveOccurred())
				Expect(envTestClient.Create(ctx, osIngressResource)).To(Succeed())

				serving := infrav1.ServingSpec{
					IngressGateway: infrav1.GatewaySpec{
						Domain: "",
					},
				}

				// when
				domain, err := serverless.FeatureData.IngressDomain.Define(&serving).Value(ctx, envTestClient)

				// then
				Expect(err).ToNot(HaveOccurred())
				Expect(domain).To(Equal("*.foo.io"))
			})

			It("should use user value when set in the DSCI", func(ctx context.Context) {
				// given
				serving := infrav1.ServingSpec{
					IngressGateway: infrav1.GatewaySpec{
						Domain: fixtures.TestDomainFooCom,
					},
				}

				// when
				domain, err := serverless.FeatureData.IngressDomain.Define(&serving).Value(ctx, envTestClient)

				// then
				Expect(err).ToNot(HaveOccurred())
				Expect(domain).To(Equal(fixtures.TestDomainFooCom))
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

			featuresProvider := func(registry feature.FeaturesRegistry) error {
				errFeatureAdd := registry.Add(
					feature.Define("tls-secret-creation").
						WithData(
							servicemesh.FeatureData.ControlPlane.Define(&dsci.Spec).AsAction(),
							serverless.FeatureData.Serving.Define(&kserveComponent.Serving).AsAction(),
							serverless.FeatureData.IngressDomain.Define(&kserveComponent.Serving).AsAction(),
							serverless.FeatureData.CertificateName.Define(&kserveComponent.Serving).AsAction(),
						).
						WithResources(serverless.ServingCertificateResource),
				)

				Expect(errFeatureAdd).ToNot(HaveOccurred())

				return nil
			}

			featuresHandler := feature.ComponentFeaturesHandler(dsci, kserveComponent.GetComponentName(), dsci.Spec.ApplicationsNamespace, featuresProvider)

			// when
			Expect(featuresHandler.Apply(ctx, envTestClient)).To(Succeed())

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

			featuresProvider := func(registry feature.FeaturesRegistry) error {
				errFeatureAdd := registry.Add(
					feature.Define("tls-secret-creation").
						WithData(
							servicemesh.FeatureData.ControlPlane.Define(&dsci.Spec).AsAction(),
							serverless.FeatureData.Serving.Define(&kserveComponent.Serving).AsAction(),
							serverless.FeatureData.IngressDomain.Define(&kserveComponent.Serving).AsAction(),
							serverless.FeatureData.CertificateName.Define(&kserveComponent.Serving).AsAction(),
						).
						WithResources(serverless.ServingCertificateResource),
				)

				Expect(errFeatureAdd).ToNot(HaveOccurred())

				return nil
			}

			featuresHandler := feature.ComponentFeaturesHandler(dsci, kserveComponent.GetComponentName(), dsci.Spec.ApplicationsNamespace, featuresProvider)

			// when
			Expect(featuresHandler.Apply(ctx, envTestClient)).To(Succeed())

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
