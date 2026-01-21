//nolint:testpackage
package modelsasservice

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

func TestGatewayValidation(t *testing.T) {
	g := NewWithT(t)

	t.Run("Gateway Validation", func(t *testing.T) {
		t.Run("should succeed when default gateway exists in the cluster", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
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
		})

		t.Run("should fail when default gateway does not exist in the cluster", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
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
		t.Run("should update AuthPolicy namespace and targetRef to default gateway", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
			}

			authPolicy := createAuthPolicy(GatewayAuthPolicyName, "wrong-namespace", "old-gateway")

			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Resources: []unstructured.Unstructured{authPolicy},
			}

			err := configureGatewayNamespaceResources(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify namespace was updated to default
			g.Expect(rr.Resources[0].GetNamespace()).Should(Equal(DefaultGatewayNamespace))

			// Verify targetRef.name was updated to default
			targetRefName, found, err := unstructured.NestedString(rr.Resources[0].Object, "spec", "targetRef", "name")
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(found).Should(BeTrue())
			g.Expect(targetRefName).Should(Equal(DefaultGatewayName))
		})

		t.Run("should succeed silently when AuthPolicy is not found in resources", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
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

			// gateway-auth-policy should be updated to defaults
			g.Expect(rr.Resources[1].GetNamespace()).Should(Equal(DefaultGatewayNamespace))
			targetRefName, _, _ := unstructured.NestedString(rr.Resources[1].Object, "spec", "targetRef", "name")
			g.Expect(targetRefName).Should(Equal(DefaultGatewayName))

			// other-policy should be unchanged
			g.Expect(rr.Resources[2].GetNamespace()).Should(Equal("keep-namespace"))
			otherTargetRefName, _, _ := unstructured.NestedString(rr.Resources[2].Object, "spec", "targetRef", "name")
			g.Expect(otherTargetRefName).Should(Equal("keep-gateway"))
		})
	})

	t.Run("Configure Gateway DestinationRule", func(t *testing.T) {
		t.Run("should update DestinationRule namespace to default gateway namespace", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
			}

			destinationRule := createDestinationRule(GatewayDestinationRuleName, "wrong-namespace")

			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Resources: []unstructured.Unstructured{destinationRule},
			}

			err := configureGatewayNamespaceResources(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify namespace was updated to default
			g.Expect(rr.Resources[0].GetNamespace()).Should(Equal(DefaultGatewayNamespace))
		})

		t.Run("should not modify DestinationRule with different name", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
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
		t.Run("should update both AuthPolicy and DestinationRule to default gateway", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
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

			// AuthPolicy should be updated to defaults
			g.Expect(rr.Resources[0].GetNamespace()).Should(Equal(DefaultGatewayNamespace))
			targetRefName, _, _ := unstructured.NestedString(rr.Resources[0].Object, "spec", "targetRef", "name")
			g.Expect(targetRefName).Should(Equal(DefaultGatewayName))

			// DestinationRule should be updated to defaults
			g.Expect(rr.Resources[1].GetNamespace()).Should(Equal(DefaultGatewayNamespace))

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
