//nolint:testpackage
package modelsasservice

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

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
					Gateway: componentApi.GatewaySpec{
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

			err := validateGateway(context.Background(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())
		})

		t.Run("should accept empty Gateway (uses defaults) when default gateway exists", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					Gateway: componentApi.GatewaySpec{},
				},
			}

			// Create a fake client with the default gateway present
			cli := createFakeClientWithGateway(DefaultGatewayNamespace, DefaultGatewayName)

			rr := &types.ReconciliationRequest{
				Instance: maas,
				Client:   cli,
			}

			err := validateGateway(context.Background(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify defaults were applied
			g.Expect(maas.Spec.Gateway.Namespace).Should(Equal(DefaultGatewayNamespace))
			g.Expect(maas.Spec.Gateway.Name).Should(Equal(DefaultGatewayName))
		})

		t.Run("should reject Gateway with only namespace specified", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					Gateway: componentApi.GatewaySpec{
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

			err := validateGateway(context.Background(), rr)
			g.Expect(err).Should(HaveOccurred())
			g.Expect(err.Error()).Should(ContainSubstring("invalid gateway specification: when specifying a custom gateway, both namespace and name must be provided"))
		})

		t.Run("should reject Gateway with only name specified", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					Gateway: componentApi.GatewaySpec{
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

			err := validateGateway(context.Background(), rr)
			g.Expect(err).Should(HaveOccurred())
			g.Expect(err.Error()).Should(ContainSubstring("invalid gateway specification: when specifying a custom gateway, both namespace and name must be provided"))
		})

		t.Run("should reject when specified Gateway does not exist in the cluster", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					Gateway: componentApi.GatewaySpec{
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

			err := validateGateway(context.Background(), rr)
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
					Gateway: componentApi.GatewaySpec{}, // Uses defaults
				},
			}

			// Create a fake client with NO gateway
			cli := createFakeClientWithoutGateway()

			rr := &types.ReconciliationRequest{
				Instance: maas,
				Client:   cli,
			}

			err := validateGateway(context.Background(), rr)
			g.Expect(err).Should(HaveOccurred())
			g.Expect(err.Error()).Should(ContainSubstring("not found"))
		})
	})
}

func TestConfigureGatewayAuthPolicy(t *testing.T) {
	g := NewWithT(t)

	t.Run("Configure Gateway AuthPolicy", func(t *testing.T) {
		t.Run("should update AuthPolicy namespace and targetRef when found", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					Gateway: componentApi.GatewaySpec{
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

			err := configureGatewayAuthPolicy(context.Background(), rr)
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
					Gateway: componentApi.GatewaySpec{
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

			err := configureGatewayAuthPolicy(context.Background(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())
		})

		t.Run("should not modify AuthPolicy with different name", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					Gateway: componentApi.GatewaySpec{
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

			err := configureGatewayAuthPolicy(context.Background(), rr)
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
					Gateway: componentApi.GatewaySpec{
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

			err := configureGatewayAuthPolicy(context.Background(), rr)
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

// createFakeClientWithGateway creates a fake client with a Gateway resource.
func createFakeClientWithGateway(namespace, name string) client.Client {
	scheme := runtime.NewScheme()
	_ = gwapiv1.AddToScheme(scheme)

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
	_ = gwapiv1.AddToScheme(scheme)

	return fake.NewClientBuilder().
		WithScheme(scheme).
		Build()
}
