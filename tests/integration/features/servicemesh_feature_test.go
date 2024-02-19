package features_test

import (
	"context"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const serviceMeshControlPlaneCRD = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  labels:
    maistra-version: 2.4.2
  annotations:
    service.beta.openshift.io/inject-cabundle: "true"
    controller-gen.kubebuilder.io/version: v0.4.1
  name: servicemeshcontrolplanes.maistra.io
spec:
  group: maistra.io
  names:
    categories:
      - maistra-io
    kind: ServiceMeshControlPlane
    listKind: ServiceMeshControlPlaneList
    plural: servicemeshcontrolplanes
    shortNames:
      - smcp
    singular: servicemeshcontrolplane
  scope: Namespaced
  versions:
    - name: v1
      schema:
        openAPIV3Schema:
          type: object
          x-kubernetes-preserve-unknown-fields: true
      served: true
      storage: false
      subresources:
        status: {}
    - name: v2
      schema:
        openAPIV3Schema:
          type: object
          x-kubernetes-preserve-unknown-fields: true
      served: true
      storage: true
      subresources:
        status: {}
`

var _ = Describe("Service Mesh feature", func() {

	var (
		dsci          *dsciv1.DSCInitialization
		objectCleaner *envtestutil.Cleaner
	)

	BeforeEach(func() {
		c, err := client.New(envTest.Config, client.Options{})
		Expect(err).ToNot(HaveOccurred())
		objectCleaner = envtestutil.CreateCleaner(c, envTest.Config, timeout, interval)

		namespace := envtestutil.AppendRandomNameTo("service-mesh-settings")

		dsci = newDSCInitialization(namespace)

		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {

	})

	Describe("preconditions", func() {

		When("operator is not installed", func() {

			It("should fail using precondition check", func() {
				// given
				featuresHandler := feature.ClusterFeaturesHandler(dsci, func(handler *feature.FeaturesHandler) error {
					verificationFeatureErr := feature.CreateFeature("no-serverless-operator-check").
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
				Expect(applyErr).To(MatchError(ContainSubstring("customresourcedefinitions.apiextensions.k8s.io \"servicemeshcontrolplanes.maistra.io\" not found")))
			})
		})

		When("operator is installed", func() {
			var smcpCrdObj *apiextensionsv1.CustomResourceDefinition

			BeforeEach(func() {
				// Create SMCP the CRD
				smcpCrdObj = &apiextensionsv1.CustomResourceDefinition{}
				Expect(yaml.Unmarshal([]byte(serviceMeshControlPlaneCRD), smcpCrdObj)).ToNot(HaveOccurred())
				c, err := client.New(envTest.Config, client.Options{})
				Expect(err).ToNot(HaveOccurred())
				Expect(c.Create(context.TODO(), smcpCrdObj)).ToNot(HaveOccurred())

				crdOptions := envtest.CRDInstallOptions{PollInterval: interval, MaxTime: timeout}
				err = envtest.WaitForCRDs(envTest.Config, []*apiextensionsv1.CustomResourceDefinition{smcpCrdObj}, crdOptions)
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				// Delete SMCP CRD
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

				ns := envtestutil.AppendRandomNameTo(testNamespacePrefix)
				nsResource := newNamespace(ns)
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
})

func getGateway(cfg *rest.Config, namespace, name string) (*unstructured.Unstructured, error) {
	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	gwGvr := schema.GroupVersionResource{
		Group:    "networking.istio.io",
		Version:  "v1beta1",
		Resource: "gateways",
	}

	gateway, err := dynamicClient.Resource(gwGvr).Namespace(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return gateway, nil
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
	smcpObj.SetGroupVersionKind(cluster.ServiceMeshControlPlaneGVK)
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
