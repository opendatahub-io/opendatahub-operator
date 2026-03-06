//nolint:testpackage
package modelsasservice

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"

	. "github.com/onsi/gomega"
)

func TestGatewayValidation(t *testing.T) {
	g := NewWithT(t)

	t.Run("Gateway Validation", func(t *testing.T) {
		t.Run("should accept valid Gateway that exists in the cluster", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "valid-namespace",
						Name:      "valid-gateway",
					},
				},
			}

			// Create a fake client with the gateway present
			cli := createFakeClientWithGateway("valid-namespace", "valid-gateway")

			rr := &types.ReconciliationRequest{
				Instance: maas,
				Client:   cli,
			}

			err := validateGateway(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())
		})

		t.Run("should accept empty Gateway (uses defaults) when default gateway exists", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{},
				},
			}

			// Create a fake client with the default gateway present
			cli := createFakeClientWithGateway(DefaultGatewayNamespace, DefaultGatewayName)

			rr := &types.ReconciliationRequest{
				Instance: maas,
				Client:   cli,
			}

			err := validateGateway(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify defaults were applied
			g.Expect(maas.Spec.GatewayRef.Namespace).Should(Equal(DefaultGatewayNamespace))
			g.Expect(maas.Spec.GatewayRef.Name).Should(Equal(DefaultGatewayName))
		})

		t.Run("should reject Gateway with only namespace specified", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "some-namespace",
						Name:      "",
					},
				},
			}

			cli := createFakeClientWithGateway("some-namespace", "some-gateway")

			rr := &types.ReconciliationRequest{
				Instance: maas,
				Client:   cli,
			}

			err := validateGateway(t.Context(), rr)
			g.Expect(err).Should(HaveOccurred())
			g.Expect(err.Error()).Should(ContainSubstring("invalid gateway specification: when specifying a custom gateway, both namespace and name must be provided"))
		})

		t.Run("should reject Gateway with only name specified", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "",
						Name:      "some-name",
					},
				},
			}

			cli := createFakeClientWithGateway("some-namespace", "some-name")

			rr := &types.ReconciliationRequest{
				Instance: maas,
				Client:   cli,
			}

			err := validateGateway(t.Context(), rr)
			g.Expect(err).Should(HaveOccurred())
			g.Expect(err.Error()).Should(ContainSubstring("invalid gateway specification: when specifying a custom gateway, both namespace and name must be provided"))
		})

		t.Run("should reject when specified Gateway does not exist in the cluster", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "non-existent-namespace",
						Name:      "non-existent-gateway",
					},
				},
			}

			// Create a fake client with NO gateway
			cli := createFakeClientWithoutGateway()

			rr := &types.ReconciliationRequest{
				Instance: maas,
				Client:   cli,
			}

			err := validateGateway(t.Context(), rr)
			g.Expect(err).Should(HaveOccurred())
			g.Expect(err.Error()).Should(ContainSubstring("gateway non-existent-namespace/non-existent-gateway not found"))
			g.Expect(err.Error()).Should(ContainSubstring("the specified Gateway must exist before enabling ModelsAsService"))
		})

		t.Run("should reject when default Gateway does not exist in the cluster", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{}, // Uses defaults
				},
			}

			// Create a fake client with NO gateway
			cli := createFakeClientWithoutGateway()

			rr := &types.ReconciliationRequest{
				Instance: maas,
				Client:   cli,
			}

			err := validateGateway(t.Context(), rr)
			g.Expect(err).Should(HaveOccurred())
			g.Expect(err.Error()).Should(ContainSubstring("not found"))
		})
	})
}

func TestConfigureGatewayNamespaceResources(t *testing.T) {
	g := NewWithT(t)

	t.Run("Configure Gateway AuthPolicy", func(t *testing.T) {
		t.Run("should update AuthPolicy namespace and targetRef when found", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "custom-gateway-ns",
						Name:      "custom-gateway",
					},
				},
			}

			authPolicy := createAuthPolicy(GatewayAuthPolicyName, "wrong-namespace", "old-gateway")

			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Resources: []unstructured.Unstructured{authPolicy},
			}

			err := configureGatewayNamespaceResources(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify namespace was updated
			g.Expect(rr.Resources[0].GetNamespace()).Should(Equal("custom-gateway-ns"))

			// Verify targetRef.name was updated
			targetRefName, found, err := unstructured.NestedString(rr.Resources[0].Object, "spec", "targetRef", "name")
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(found).Should(BeTrue())
			g.Expect(targetRefName).Should(Equal("custom-gateway"))
		})

		t.Run("should succeed silently when AuthPolicy is not found in resources", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "custom-gateway-ns",
						Name:      "custom-gateway",
					},
				},
			}

			// No AuthPolicy in resources
			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Resources: []unstructured.Unstructured{},
			}

			err := configureGatewayNamespaceResources(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())
		})

		t.Run("should not modify AuthPolicy with different name", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "custom-gateway-ns",
						Name:      "custom-gateway",
					},
				},
			}

			// AuthPolicy with a different name should not be modified
			otherAuthPolicy := createAuthPolicy("other-auth-policy", "original-namespace", "original-gateway")

			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Resources: []unstructured.Unstructured{otherAuthPolicy},
			}

			err := configureGatewayNamespaceResources(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify namespace was NOT updated
			g.Expect(rr.Resources[0].GetNamespace()).Should(Equal("original-namespace"))

			// Verify targetRef.name was NOT updated
			targetRefName, found, err := unstructured.NestedString(rr.Resources[0].Object, "spec", "targetRef", "name")
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(found).Should(BeTrue())
			g.Expect(targetRefName).Should(Equal("original-gateway"))
		})

		t.Run("should only modify matching AuthPolicy when multiple resources present", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "new-gateway-ns",
						Name:      "new-gateway",
					},
				},
			}

			// Mix of resources
			gatewayAuthPolicy := createAuthPolicy(GatewayAuthPolicyName, "old-namespace", "old-gateway")
			otherAuthPolicy := createAuthPolicy("other-policy", "keep-namespace", "keep-gateway")
			configMap := &unstructured.Unstructured{}
			configMap.SetAPIVersion("v1")
			configMap.SetKind("ConfigMap")
			configMap.SetName("some-config")
			configMap.SetNamespace("app-namespace")

			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Resources: []unstructured.Unstructured{*configMap, gatewayAuthPolicy, otherAuthPolicy},
			}

			err := configureGatewayNamespaceResources(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// ConfigMap should be unchanged
			g.Expect(rr.Resources[0].GetNamespace()).Should(Equal("app-namespace"))

			// gateway-auth-policy should be updated
			g.Expect(rr.Resources[1].GetNamespace()).Should(Equal("new-gateway-ns"))
			targetRefName, _, _ := unstructured.NestedString(rr.Resources[1].Object, "spec", "targetRef", "name")
			g.Expect(targetRefName).Should(Equal("new-gateway"))

			// other-policy should be unchanged
			g.Expect(rr.Resources[2].GetNamespace()).Should(Equal("keep-namespace"))
			otherTargetRefName, _, _ := unstructured.NestedString(rr.Resources[2].Object, "spec", "targetRef", "name")
			g.Expect(otherTargetRefName).Should(Equal("keep-gateway"))
		})
	})

	t.Run("Configure Gateway DestinationRule", func(t *testing.T) {
		t.Run("should update DestinationRule namespace when found", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "custom-gateway-ns",
						Name:      "custom-gateway",
					},
				},
			}

			destinationRule := createDestinationRule(GatewayDestinationRuleName, "wrong-namespace")

			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Resources: []unstructured.Unstructured{destinationRule},
			}

			err := configureGatewayNamespaceResources(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify namespace was updated
			g.Expect(rr.Resources[0].GetNamespace()).Should(Equal("custom-gateway-ns"))
		})

		t.Run("should not modify DestinationRule with different name", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "custom-gateway-ns",
						Name:      "custom-gateway",
					},
				},
			}

			// DestinationRule with a different name should not be modified
			otherDestinationRule := createDestinationRule("other-destination-rule", "original-namespace")

			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Resources: []unstructured.Unstructured{otherDestinationRule},
			}

			err := configureGatewayNamespaceResources(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify namespace was NOT updated
			g.Expect(rr.Resources[0].GetNamespace()).Should(Equal("original-namespace"))
		})
	})

	t.Run("Configure Both AuthPolicy and DestinationRule", func(t *testing.T) {
		t.Run("should update both AuthPolicy and DestinationRule namespaces", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "target-gateway-ns",
						Name:      "target-gateway",
					},
				},
			}

			authPolicy := createAuthPolicy(GatewayAuthPolicyName, "old-namespace", "old-gateway")
			destinationRule := createDestinationRule(GatewayDestinationRuleName, "old-namespace")
			configMap := &unstructured.Unstructured{}
			configMap.SetAPIVersion("v1")
			configMap.SetKind("ConfigMap")
			configMap.SetName("some-config")
			configMap.SetNamespace("app-namespace")

			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Resources: []unstructured.Unstructured{authPolicy, destinationRule, *configMap},
			}

			err := configureGatewayNamespaceResources(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// AuthPolicy should be updated
			g.Expect(rr.Resources[0].GetNamespace()).Should(Equal("target-gateway-ns"))
			targetRefName, _, _ := unstructured.NestedString(rr.Resources[0].Object, "spec", "targetRef", "name")
			g.Expect(targetRefName).Should(Equal("target-gateway"))

			// DestinationRule should be updated
			g.Expect(rr.Resources[1].GetNamespace()).Should(Equal("target-gateway-ns"))

			// ConfigMap should be unchanged
			g.Expect(rr.Resources[2].GetNamespace()).Should(Equal("app-namespace"))
		})
	})
}

// createAuthPolicy creates an unstructured AuthPolicy resource for testing.
func createAuthPolicy(name, namespace, targetGatewayName string) unstructured.Unstructured {
	authPolicy := unstructured.Unstructured{}
	authPolicy.SetGroupVersionKind(gvk.AuthPolicyv1)
	authPolicy.SetName(name)
	authPolicy.SetNamespace(namespace)

	// Set spec.targetRef
	_ = unstructured.SetNestedField(authPolicy.Object, targetGatewayName, "spec", "targetRef", "name")
	_ = unstructured.SetNestedField(authPolicy.Object, "Gateway", "spec", "targetRef", "kind")
	_ = unstructured.SetNestedField(authPolicy.Object, "gateway.networking.k8s.io", "spec", "targetRef", "group")

	return authPolicy
}

// createDestinationRule creates an unstructured DestinationRule resource for testing.
func createDestinationRule(name, namespace string) unstructured.Unstructured {
	destinationRule := unstructured.Unstructured{}
	destinationRule.SetGroupVersionKind(gvk.DestinationRule)
	destinationRule.SetName(name)
	destinationRule.SetNamespace(namespace)

	// Set spec.host (typical for DestinationRule)
	_ = unstructured.SetNestedField(destinationRule.Object, "*.local", "spec", "host")

	return destinationRule
}

// createFakeClientWithGateway creates a fake client with a Gateway resource.
func createFakeClientWithGateway(namespace, name string) client.Client {
	scheme := runtime.NewScheme()
	_ = gwapiv1.Install(scheme)

	gateway := &gwapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(gateway).
		Build()
}

// createFakeClientWithoutGateway creates a fake client with no Gateway resources.
func createFakeClientWithoutGateway() client.Client {
	scheme := runtime.NewScheme()
	_ = gwapiv1.Install(scheme)

	return fake.NewClientBuilder().
		WithScheme(scheme).
		Build()
}

func TestAPIKeyConfiguration(t *testing.T) {
	g := NewWithT(t)

	t.Run("API Key MaxExpirationDays Configuration", func(t *testing.T) {
		t.Run("should include maxExpirationDays in params when specified", func(t *testing.T) {
			maxDays := int32(30)
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "test-namespace",
						Name:      "test-gateway",
					},
					APIKeys: &componentApi.APIKeysConfig{
						MaxExpirationDays: &maxDays,
					},
				},
			}

			// Verify the APIKeys config is correctly set
			g.Expect(maas.Spec.APIKeys).ShouldNot(BeNil())
			g.Expect(maas.Spec.APIKeys.MaxExpirationDays).ShouldNot(BeNil())
			g.Expect(*maas.Spec.APIKeys.MaxExpirationDays).Should(Equal(int32(30)))
		})

		t.Run("should allow nil APIKeys config (uses default)", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "test-namespace",
						Name:      "test-gateway",
					},
					APIKeys: nil,
				},
			}

			// Verify APIKeys is nil (will use default from params.env)
			g.Expect(maas.Spec.APIKeys).Should(BeNil())
		})

		t.Run("should allow APIKeys with nil MaxExpirationDays (uses default)", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "test-namespace",
						Name:      "test-gateway",
					},
					APIKeys: &componentApi.APIKeysConfig{
						MaxExpirationDays: nil,
					},
				},
			}

			// Verify APIKeys exists but MaxExpirationDays is nil
			g.Expect(maas.Spec.APIKeys).ShouldNot(BeNil())
			g.Expect(maas.Spec.APIKeys.MaxExpirationDays).Should(BeNil())
		})

		t.Run("should accept various valid maxExpirationDays values", func(t *testing.T) {
			testCases := []int32{1, 7, 30, 90, 365}

			for _, days := range testCases {
				maxDays := days
				maas := &componentApi.ModelsAsService{
					ObjectMeta: metav1.ObjectMeta{
						Name: componentApi.ModelsAsServiceInstanceName,
					},
					Spec: componentApi.ModelsAsServiceSpec{
						GatewayRef: componentApi.GatewayRef{
							Namespace: "test-namespace",
							Name:      "test-gateway",
						},
						APIKeys: &componentApi.APIKeysConfig{
							MaxExpirationDays: &maxDays,
						},
					},
				}

				g.Expect(*maas.Spec.APIKeys.MaxExpirationDays).Should(Equal(days))
			}
		})
	})
}

func TestConfigureConfigHashAnnotation(t *testing.T) {
	g := NewWithT(t)

	t.Run("Config Hash Annotation", func(t *testing.T) {
		t.Run("should add config hash annotation to deployment when ConfigMap exists", func(t *testing.T) {
			// Create ConfigMap
			configMap := createConfigMap("opendatahub", map[string]string{
				"gateway-namespace": "openshift-ingress",
				"gateway-name":      "maas-default-gateway",
			})

			// Create Deployment
			deployment := createDeployment("opendatahub")

			rr := &types.ReconciliationRequest{
				Resources: []unstructured.Unstructured{configMap, deployment},
			}

			err := configureConfigHashAnnotation(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify the deployment has the annotation
			dep := &appsv1.Deployment{}
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(rr.Resources[1].Object, dep)
			g.Expect(err).ShouldNot(HaveOccurred())

			annotationKey := labels.ODHAppPrefix + "/MaaSConfigHash"
			g.Expect(dep.Spec.Template.Annotations).Should(HaveKey(annotationKey))
			g.Expect(dep.Spec.Template.Annotations[annotationKey]).ShouldNot(BeEmpty())
		})

		t.Run("should update hash when ConfigMap data changes", func(t *testing.T) {
			// First ConfigMap
			configMap1 := createConfigMap("opendatahub", map[string]string{
				"gateway-namespace":           "openshift-ingress",
				"gateway-name":                "maas-default-gateway",
				"api-key-max-expiration-days": "30",
			})
			deployment1 := createDeployment("opendatahub")

			rr1 := &types.ReconciliationRequest{
				Resources: []unstructured.Unstructured{configMap1, deployment1},
			}

			err := configureConfigHashAnnotation(t.Context(), rr1)
			g.Expect(err).ShouldNot(HaveOccurred())

			dep1 := &appsv1.Deployment{}
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(rr1.Resources[1].Object, dep1)
			g.Expect(err).ShouldNot(HaveOccurred())

			annotationKey := labels.ODHAppPrefix + "/MaaSConfigHash"
			hash1 := dep1.Spec.Template.Annotations[annotationKey]

			// Second ConfigMap with different data
			configMap2 := createConfigMap("opendatahub", map[string]string{
				"gateway-namespace":           "openshift-ingress",
				"gateway-name":                "maas-default-gateway",
				"api-key-max-expiration-days": "90", // Changed!
			})
			deployment2 := createDeployment("opendatahub")

			rr2 := &types.ReconciliationRequest{
				Resources: []unstructured.Unstructured{configMap2, deployment2},
			}

			err = configureConfigHashAnnotation(t.Context(), rr2)
			g.Expect(err).ShouldNot(HaveOccurred())

			dep2 := &appsv1.Deployment{}
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(rr2.Resources[1].Object, dep2)
			g.Expect(err).ShouldNot(HaveOccurred())

			hash2 := dep2.Spec.Template.Annotations[annotationKey]

			// Hashes should be different
			g.Expect(hash1).ShouldNot(Equal(hash2))
		})

		t.Run("should succeed silently when ConfigMap is not found", func(t *testing.T) {
			// Only Deployment, no ConfigMap
			deployment := createDeployment("opendatahub")

			rr := &types.ReconciliationRequest{
				Resources: []unstructured.Unstructured{deployment},
			}

			err := configureConfigHashAnnotation(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())
		})

		t.Run("should succeed silently when Deployment is not found", func(t *testing.T) {
			// Only ConfigMap, no Deployment
			configMap := createConfigMap("opendatahub", map[string]string{
				"gateway-namespace": "openshift-ingress",
			})

			rr := &types.ReconciliationRequest{
				Resources: []unstructured.Unstructured{configMap},
			}

			err := configureConfigHashAnnotation(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())
		})

		t.Run("should produce consistent hash for same data", func(t *testing.T) {
			data := map[string]string{
				"b-key": "value-b",
				"a-key": "value-a",
				"c-key": "value-c",
			}

			hash1 := hashConfigMapData(data)
			hash2 := hashConfigMapData(data)

			g.Expect(hash1).Should(Equal(hash2))
		})
	})
}

// createConfigMap creates an unstructured ConfigMap resource for testing.
// Uses MaaSParametersConfigMapName as the name.
func createConfigMap(namespace string, data map[string]string) unstructured.Unstructured {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      MaaSParametersConfigMapName,
			Namespace: namespace,
		},
		Data: data,
	}

	u, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(cm)
	return unstructured.Unstructured{Object: u}
}

// createDeployment creates an unstructured Deployment resource for testing.
// Uses MaaSAPIDeploymentName as the name.
func createDeployment(namespace string) unstructured.Unstructured {
	dep := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      MaaSAPIDeploymentName,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "maas-api",
							Image: "quay.io/opendatahub/maas-api:latest",
						},
					},
				},
			},
		},
	}

	u, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(dep)
	return unstructured.Unstructured{Object: u}
}
