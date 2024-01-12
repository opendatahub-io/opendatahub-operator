package features_test

import (
	"context"
	"fmt"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/servicemesh"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/gvr"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const smcpCrd = `apiVersion: apiextensions.k8s.io/v1
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
	var testFeature *feature.Feature
	var objectCleaner *envtestutil.Cleaner

	BeforeEach(func() {
		c, err := client.New(envTest.Config, client.Options{})
		Expect(err).ToNot(HaveOccurred())

		objectCleaner = envtestutil.CreateCleaner(c, envTest.Config, timeout, interval)

		testFeatureName := "servicemesh-feature"
		namespace := envtestutil.AppendRandomNameTo(testFeatureName)

		dsciSpec := newDSCInitializationSpec(namespace)
		testFeature, err = feature.CreateFeature(testFeatureName).
			For(dsciSpec).
			UsingConfig(envTest.Config).
			Load()

		Expect(err).ToNot(HaveOccurred())
	})

	Describe("preconditions", func() {
		When("operator is not installed", func() {
			It("operator presence check should return an error", func() {
				Expect(servicemesh.EnsureServiceMeshOperatorInstalled(testFeature)).To(HaveOccurred())
			})
		})
		When("operator is installed", func() {
			var smcpCrdObj *apiextensionsv1.CustomResourceDefinition

			BeforeEach(func() {
				// Create SMCP the CRD
				smcpCrdObj = &apiextensionsv1.CustomResourceDefinition{}
				Expect(yaml.Unmarshal([]byte(smcpCrd), smcpCrdObj)).ToNot(HaveOccurred())
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
			It("operator presence check should succeed", func() {
				Expect(servicemesh.EnsureServiceMeshOperatorInstalled(testFeature)).To(Succeed())
			})
			It("should find installed Service Mesh Control Plane", func() {
				c, err := client.New(envTest.Config, client.Options{})
				Expect(err).ToNot(HaveOccurred())

				ns := envtestutil.AppendRandomNameTo(testNamespacePrefix)
				nsResource := createNamespace(ns)
				Expect(c.Create(context.Background(), nsResource)).To(Succeed())
				defer objectCleaner.DeleteAll(nsResource)

				createServiceMeshControlPlane("test-name", ns)

				testFeature.Spec.ControlPlane.Namespace = ns
				testFeature.Spec.ControlPlane.Name = "test-name"
				Expect(servicemesh.EnsureServiceMeshInstalled(testFeature)).To(Succeed())
			})
			It("should fail to find Service Mesh Control Plane if not present", func() {
				Expect(servicemesh.EnsureServiceMeshInstalled(testFeature)).ToNot(Succeed())
			})
		})
	})
})

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
	Expect(createSMCPInCluster(envTest.Config, serviceMeshControlPlane, namespace)).To(Succeed())
}

// createSMCPInCluster uses dynamic client to create a dummy SMCP resource for testing.
func createSMCPInCluster(cfg *rest.Config, smcpObj *unstructured.Unstructured, namespace string) error {
	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return err
	}

	result, err := dynamicClient.Resource(gvr.SMCP).Namespace(namespace).Create(context.TODO(), smcpObj, metav1.CreateOptions{})
	if err != nil {
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

	if err := unstructured.SetNestedField(result.Object, status, "status"); err != nil {
		return err
	}

	r, err := dynamicClient.Resource(gvr.SMCP).Namespace(namespace).UpdateStatus(context.TODO(), result, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	fmt.Printf("result: %v", r)

	return nil
}
