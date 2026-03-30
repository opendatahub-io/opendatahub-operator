//nolint:testpackage
package modelsasservice

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"

	. "github.com/onsi/gomega"
)

func boolPtr(v bool) *bool { return &v }

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

func TestBuildTelemetryLabels(t *testing.T) {
	g := NewWithT(t)

	t.Run("Telemetry Labels", func(t *testing.T) {
		t.Run("should return defaults when config is nil", func(t *testing.T) {
			labels := buildTelemetryLabels(logr.Discard(), nil)

			// Always-on dimensions - verify both key presence and CEL expression values
			g.Expect(labels).Should(HaveKey("subscription"))
			g.Expect(labels["subscription"]).Should(Equal("auth.identity.selected_subscription"))
			g.Expect(labels).Should(HaveKey("cost_center"))
			g.Expect(labels["cost_center"]).Should(Equal("auth.identity.costCenter"))
			g.Expect(labels).Should(HaveKey("tier"))
			g.Expect(labels["tier"]).Should(Equal("auth.identity.tier"))

			// Default enabled dimensions - verify both key presence and CEL expression values
			g.Expect(labels).Should(HaveKey("organization_id"))
			g.Expect(labels["organization_id"]).Should(Equal("auth.identity.organizationId"))
			g.Expect(labels).Should(HaveKey("model"))
			g.Expect(labels["model"]).Should(Equal(`responseBodyJSON("/model")`))

			// Default disabled dimensions (user disabled for GDPR compliance)
			g.Expect(labels).ShouldNot(HaveKey("user"))

			// Default disabled dimensions
			g.Expect(labels).ShouldNot(HaveKey("group"))
		})

		t.Run("should return defaults when metrics config is nil", func(t *testing.T) {
			config := &componentApi.TelemetryConfig{
				Metrics: nil,
			}

			labels := buildTelemetryLabels(logr.Discard(), config)

			// Should have 5 labels (3 always-on + 2 default enabled)
			g.Expect(labels).Should(HaveLen(5))
			g.Expect(labels).ShouldNot(HaveKey("group"))
		})

		t.Run("should handle capture flags", func(t *testing.T) {
			testCases := []struct {
				name           string
				config         *componentApi.MetricsConfig
				expectedKeys   []string
				unexpectedKeys []string
			}{
				{
					name:           "captureGroup enabled",
					config:         &componentApi.MetricsConfig{CaptureGroup: boolPtr(true)},
					expectedKeys:   []string{"group"},
					unexpectedKeys: nil,
				},
				{
					name:           "captureUser disabled",
					config:         &componentApi.MetricsConfig{CaptureUser: boolPtr(false)},
					expectedKeys:   nil,
					unexpectedKeys: []string{"user"},
				},
				{
					name:           "captureOrganization disabled",
					config:         &componentApi.MetricsConfig{CaptureOrganization: boolPtr(false)},
					expectedKeys:   nil,
					unexpectedKeys: []string{"organization_id"},
				},
				{
					name:           "captureModelUsage disabled",
					config:         &componentApi.MetricsConfig{CaptureModelUsage: boolPtr(false)},
					expectedKeys:   nil,
					unexpectedKeys: []string{"model"},
				},
			}

			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					config := &componentApi.TelemetryConfig{Metrics: tc.config}
					labels := buildTelemetryLabels(logr.Discard(), config)

					for _, key := range tc.expectedKeys {
						g.Expect(labels).Should(HaveKey(key))
						// Verify CEL expression values for enabled dimensions
						switch key {
						case "group":
							g.Expect(labels[key]).Should(Equal("auth.identity.group"))
						case "user":
							g.Expect(labels[key]).Should(Equal("auth.identity.userid"))
						case "organization_id":
							g.Expect(labels[key]).Should(Equal("auth.identity.organizationId"))
						case "model":
							g.Expect(labels[key]).Should(Equal(`responseBodyJSON("/model")`))
						}
					}
					for _, key := range tc.unexpectedKeys {
						g.Expect(labels).ShouldNot(HaveKey(key))
					}
				})
			}
		})

		t.Run("should handle extreme configurations", func(t *testing.T) {
			testCases := []struct {
				name         string
				config       *componentApi.MetricsConfig
				expectedLen  int
				alwaysOnKeys []string
			}{
				{
					name: "all dimensions disabled",
					config: &componentApi.MetricsConfig{
						CaptureOrganization: boolPtr(false),
						CaptureUser:         boolPtr(false),
						CaptureGroup:        boolPtr(false),
						CaptureModelUsage:   boolPtr(false),
					},
					expectedLen:  3,
					alwaysOnKeys: []string{"subscription", "cost_center", "tier"},
				},
				{
					name: "all dimensions enabled",
					config: &componentApi.MetricsConfig{
						CaptureOrganization: boolPtr(true),
						CaptureUser:         boolPtr(true),
						CaptureGroup:        boolPtr(true),
						CaptureModelUsage:   boolPtr(true),
					},
					expectedLen:  7,
					alwaysOnKeys: []string{"subscription", "cost_center", "tier", "organization_id", "user", "group", "model"},
				},
			}

			for _, tc := range testCases {
				t.Run(tc.name, func(t *testing.T) {
					config := &componentApi.TelemetryConfig{Metrics: tc.config}
					labels := buildTelemetryLabels(logr.Discard(), config)

					g.Expect(labels).Should(HaveLen(tc.expectedLen))
					for _, key := range tc.alwaysOnKeys {
						g.Expect(labels).Should(HaveKey(key))
					}
				})
			}
		})

		t.Run("should have correct CEL expression values for all dimensions", func(t *testing.T) {
			config := &componentApi.TelemetryConfig{
				Metrics: &componentApi.MetricsConfig{
					CaptureOrganization: boolPtr(true),
					CaptureUser:         boolPtr(true),
					CaptureGroup:        boolPtr(true),
					CaptureModelUsage:   boolPtr(true),
				},
			}
			labels := buildTelemetryLabels(logr.Discard(), config)

			// Verify all CEL expression values are correct
			expectedValues := map[string]string{
				"subscription":    "auth.identity.selected_subscription",
				"cost_center":     "auth.identity.costCenter",
				"tier":            "auth.identity.tier",
				"organization_id": "auth.identity.organizationId",
				"user":            "auth.identity.userid",
				"group":           "auth.identity.group",
				"model":           `responseBodyJSON("/model")`,
			}

			for key, expectedValue := range expectedValues {
				g.Expect(labels).Should(HaveKey(key))
				g.Expect(labels[key]).Should(Equal(expectedValue), "CEL expression for %s should match", key)
			}
		})
	})
}

func TestConfigureTelemetryPolicy(t *testing.T) {
	g := NewWithT(t)

	t.Run("Error Handling", func(t *testing.T) {
		t.Run("should return error for wrong instance type", func(t *testing.T) {
			rr := &types.ReconciliationRequest{
				Instance:  &componentApi.Dashboard{}, // wrong type
				Resources: []unstructured.Unstructured{},
			}
			err := configureTelemetryPolicyCore(t.Context(), rr)
			g.Expect(err).Should(HaveOccurred())
			g.Expect(err.Error()).Should(ContainSubstring("is not a componentApi.ModelsAsService"))
		})

		t.Run("should skip when TelemetryPolicy CRD is not available", func(t *testing.T) {
			// Create a basic fake client without TelemetryPolicy CRD
			cli := createFakeClientWithoutGateway() // This won't have TelemetryPolicy CRD

			rr := &types.ReconciliationRequest{
				Instance:  &componentApi.ModelsAsService{},
				Client:    cli,
				Resources: []unstructured.Unstructured{},
			}

			err := configureTelemetryPolicy(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())  // Should succeed but skip
			g.Expect(rr.Resources).Should(BeEmpty()) // No resources should be added
		})
	})

	t.Run("TelemetryPolicy Creation", func(t *testing.T) {
		t.Run("should create TelemetryPolicy with correct metadata", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "components.platform.opendatahub.io/v1alpha1",
					Kind:       "ModelsAsService",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
					UID:  "test-uid-123",
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "test-gateway-ns",
						Name:      "test-gateway",
					},
				},
			}

			// Use a basic fake client - we'll test the core logic without CRD availability check
			cli, err := fakeclient.New()
			g.Expect(err).ShouldNot(HaveOccurred())

			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Client:    cli,
				Resources: []unstructured.Unstructured{},
			}

			// Test the core TelemetryPolicy creation logic directly, bypassing the CRD check
			// This tests the business logic without the defensive CRD availability check
			err = configureTelemetryPolicyCore(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Should have added one resource
			g.Expect(rr.Resources).Should(HaveLen(1))

			policy := rr.Resources[0]
			g.Expect(policy.GetName()).Should(Equal(TelemetryPolicyName))
			g.Expect(policy.GetNamespace()).Should(Equal("test-gateway-ns"))
			g.Expect(policy.GetKind()).Should(Equal("TelemetryPolicy"))
			g.Expect(policy.GetAPIVersion()).Should(Equal("extensions.kuadrant.io/v1alpha1"))

			// Check OwnerReferences
			ownerRefs := policy.GetOwnerReferences()
			g.Expect(ownerRefs).ShouldNot(BeEmpty())
			g.Expect(ownerRefs).Should(HaveLen(1))
			g.Expect(ownerRefs[0].Name).Should(Equal(componentApi.ModelsAsServiceInstanceName))
			g.Expect(ownerRefs[0].Kind).Should(Equal("ModelsAsService"))
			g.Expect(ownerRefs[0].APIVersion).Should(Equal("components.platform.opendatahub.io/v1alpha1"))
			g.Expect(string(ownerRefs[0].UID)).Should(Equal("test-uid-123"))
			g.Expect(ownerRefs[0].Controller).ShouldNot(BeNil())
			g.Expect(*ownerRefs[0].Controller).Should(BeTrue())
			g.Expect(ownerRefs[0].BlockOwnerDeletion).ShouldNot(BeNil())
			g.Expect(*ownerRefs[0].BlockOwnerDeletion).Should(BeTrue())
		})

		t.Run("should set targetRef to configured gateway", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "my-ns",
						Name:      "my-gateway",
					},
				},
			}

			// Use basic fake client for testing core logic
			cli, err := fakeclient.New()
			g.Expect(err).ShouldNot(HaveOccurred())

			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Client:    cli,
				Resources: []unstructured.Unstructured{},
			}

			err = configureTelemetryPolicyCore(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			targetName, found, err := unstructured.NestedString(
				rr.Resources[0].Object, "spec", "targetRef", "name")
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(found).Should(BeTrue())
			g.Expect(targetName).Should(Equal("my-gateway"))

			targetKind, _, _ := unstructured.NestedString(
				rr.Resources[0].Object, "spec", "targetRef", "kind")
			g.Expect(targetKind).Should(Equal("Gateway"))
		})

		t.Run("should apply telemetry config to labels", func(t *testing.T) {
			captureUser := false
			captureGroup := true
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "ns",
						Name:      "gw",
					},
					Telemetry: &componentApi.TelemetryConfig{
						Metrics: &componentApi.MetricsConfig{
							CaptureUser:  &captureUser,
							CaptureGroup: &captureGroup,
						},
					},
				},
			}

			// Use basic fake client for testing core logic
			cli, err := fakeclient.New()
			g.Expect(err).ShouldNot(HaveOccurred())

			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Client:    cli,
				Resources: []unstructured.Unstructured{},
			}

			err = configureTelemetryPolicyCore(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			labels, found, err := unstructured.NestedMap(
				rr.Resources[0].Object, "spec", "metrics", "default", "labels")
			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(found).Should(BeTrue())

			// user should be excluded
			g.Expect(labels).ShouldNot(HaveKey("user"))
			// group should be included
			g.Expect(labels).Should(HaveKey("group"))
			// always-on dimensions should be present
			g.Expect(labels).Should(HaveKey("subscription"))
			g.Expect(labels).Should(HaveKey("cost_center"))
			g.Expect(labels).Should(HaveKey("tier"))
		})

		t.Run("should append to existing resources", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "ns",
						Name:      "gw",
					},
				},
			}

			// Start with an existing resource
			existingResource := unstructured.Unstructured{}
			existingResource.SetAPIVersion("v1")
			existingResource.SetKind("ConfigMap")
			existingResource.SetName("existing-config")

			// Use basic fake client for testing core logic
			cli, err := fakeclient.New()
			g.Expect(err).ShouldNot(HaveOccurred())

			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Client:    cli,
				Resources: []unstructured.Unstructured{existingResource},
			}

			err = configureTelemetryPolicyCore(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Should have 2 resources now
			g.Expect(rr.Resources).Should(HaveLen(2))
			g.Expect(rr.Resources[0].GetName()).Should(Equal("existing-config"))
			g.Expect(rr.Resources[1].GetName()).Should(Equal(TelemetryPolicyName))
		})

		t.Run("should use default telemetry config when not specified", func(t *testing.T) {
			maas := &componentApi.ModelsAsService{
				ObjectMeta: metav1.ObjectMeta{
					Name: componentApi.ModelsAsServiceInstanceName,
				},
				Spec: componentApi.ModelsAsServiceSpec{
					GatewayRef: componentApi.GatewayRef{
						Namespace: "ns",
						Name:      "gw",
					},
					Telemetry: nil, // No telemetry config
				},
			}

			// Use basic fake client for testing core logic
			cli, err := fakeclient.New()
			g.Expect(err).ShouldNot(HaveOccurred())

			rr := &types.ReconciliationRequest{
				Instance:  maas,
				Client:    cli,
				Resources: []unstructured.Unstructured{},
			}

			err = configureTelemetryPolicyCore(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			labels, _, _ := unstructured.NestedMap(
				rr.Resources[0].Object, "spec", "metrics", "default", "labels")

			// Should have default labels (5 total: 3 always-on + 2 default enabled)
			g.Expect(labels).Should(HaveLen(5))
			g.Expect(labels).Should(HaveKey("subscription"))
			g.Expect(labels).Should(HaveKey("organization_id"))
			g.Expect(labels).Should(HaveKey("model"))
			g.Expect(labels).ShouldNot(HaveKey("user"))  // Disabled by default (GDPR)
			g.Expect(labels).ShouldNot(HaveKey("group")) // Disabled by default
		})
	})
}

func TestConfigureConfigHashAnnotation(t *testing.T) {
	g := NewWithT(t)

	t.Run("Config Hash Annotation", func(t *testing.T) {
		t.Run("should add config hash annotation to deployment when ConfigMap exists", func(t *testing.T) {
			// Create ConfigMap
			configMap := createConfigMap(map[string]string{
				"gateway-namespace": "openshift-ingress",
				"gateway-name":      "maas-default-gateway",
			})

			// Create Deployment
			deployment := createDeployment()

			rr := &types.ReconciliationRequest{
				Resources: []unstructured.Unstructured{configMap, deployment},
			}

			err := configureConfigHashAnnotation(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			// Verify the deployment has the annotation
			dep := &appsv1.Deployment{}
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(rr.Resources[1].Object, dep)
			g.Expect(err).ShouldNot(HaveOccurred())

			annotationKey := labels.ODHAppPrefix + "/maas-config-hash"
			g.Expect(dep.Spec.Template.Annotations).Should(HaveKey(annotationKey))
			g.Expect(dep.Spec.Template.Annotations[annotationKey]).ShouldNot(BeEmpty())
		})

		t.Run("should update hash when ConfigMap data changes", func(t *testing.T) {
			// First ConfigMap
			configMap1 := createConfigMap(map[string]string{
				"gateway-namespace":           "openshift-ingress",
				"gateway-name":                "maas-default-gateway",
				"api-key-max-expiration-days": "30",
			})
			deployment1 := createDeployment()

			rr1 := &types.ReconciliationRequest{
				Resources: []unstructured.Unstructured{configMap1, deployment1},
			}

			err := configureConfigHashAnnotation(t.Context(), rr1)
			g.Expect(err).ShouldNot(HaveOccurred())

			dep1 := &appsv1.Deployment{}
			err = runtime.DefaultUnstructuredConverter.FromUnstructured(rr1.Resources[1].Object, dep1)
			g.Expect(err).ShouldNot(HaveOccurred())

			annotationKey := labels.ODHAppPrefix + "/maas-config-hash"
			hash1 := dep1.Spec.Template.Annotations[annotationKey]

			// Second ConfigMap with different data
			configMap2 := createConfigMap(map[string]string{
				"gateway-namespace":           "openshift-ingress",
				"gateway-name":                "maas-default-gateway",
				"api-key-max-expiration-days": "90", // Changed!
			})
			deployment2 := createDeployment()

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
			deployment := createDeployment()

			rr := &types.ReconciliationRequest{
				Resources: []unstructured.Unstructured{deployment},
			}

			err := configureConfigHashAnnotation(t.Context(), rr)
			g.Expect(err).ShouldNot(HaveOccurred())
		})

		t.Run("should succeed silently when Deployment is not found", func(t *testing.T) {
			// Only ConfigMap, no Deployment
			configMap := createConfigMap(map[string]string{
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
// Uses MaaSParametersConfigMapName and "opendatahub" namespace.
func createConfigMap(data map[string]string) unstructured.Unstructured {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      MaaSParametersConfigMapName,
			Namespace: "opendatahub",
		},
		Data: data,
	}

	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(cm)
	if err != nil {
		panic(fmt.Sprintf("failed to convert ConfigMap to unstructured: %v", err))
	}
	return unstructured.Unstructured{Object: u}
}

// createDeployment creates an unstructured Deployment resource for testing.
// Uses MaaSAPIDeploymentName and "opendatahub" namespace.
// Note: Template.Annotations is left nil to test the nil-initialization branch.
func createDeployment() unstructured.Unstructured {
	dep := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      MaaSAPIDeploymentName,
			Namespace: "opendatahub",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{},
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

	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(dep)
	if err != nil {
		panic(fmt.Sprintf("failed to convert Deployment to unstructured: %v", err))
	}
	return unstructured.Unstructured{Object: u}
}

func TestConfigureExternalOIDC(t *testing.T) {
	g := NewWithT(t)

	t.Run("should patch AuthPolicy with OIDC rules when externalOIDC is set", func(t *testing.T) {
		maas := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.ModelsAsServiceInstanceName,
			},
			Spec: componentApi.ModelsAsServiceSpec{
				ExternalOIDC: &componentApi.ExternalOIDCConfig{
					IssuerURL: "https://keycloak.example.com/realms/maas",
					ClientID:  "maas-cli",
					TTL:       300,
				},
			},
		}

		authPolicy := createAuthPolicy(MaaSAPIAuthPolicyName, "opendatahub", "maas-api-route")

		rr := &types.ReconciliationRequest{
			Instance:  maas,
			Resources: []unstructured.Unstructured{authPolicy},
		}

		err := configureExternalOIDC(t.Context(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// oidc-identities authentication should be added
		issuer, found, err := unstructured.NestedString(rr.Resources[0].Object,
			"spec", "rules", "authentication", "oidc-identities", "jwt", "issuerUrl")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).Should(BeTrue())
		g.Expect(issuer).Should(Equal("https://keycloak.example.com/realms/maas"))

		// openshift-identities priority should be bumped to 2
		priority, found, err := unstructured.NestedFieldNoCopy(rr.Resources[0].Object,
			"spec", "rules", "authentication", "openshift-identities", "priority")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).Should(BeTrue())
		g.Expect(priority).Should(Equal(int64(2)))

		// oidc-client-bound authorization should be added
		patterns, found, err := unstructured.NestedSlice(rr.Resources[0].Object,
			"spec", "rules", "authorization", "oidc-client-bound", "patternMatching", "patterns")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).Should(BeTrue())
		g.Expect(patterns).Should(HaveLen(1))
		pattern, ok := patterns[0].(map[string]interface{})
		g.Expect(ok).Should(BeTrue())
		g.Expect(pattern["value"]).Should(Equal("maas-cli"))
	})

	t.Run("should not modify AuthPolicy when externalOIDC is nil", func(t *testing.T) {
		maas := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.ModelsAsServiceInstanceName,
			},
			Spec: componentApi.ModelsAsServiceSpec{},
		}

		authPolicy := createAuthPolicy(MaaSAPIAuthPolicyName, "opendatahub", "maas-api-route")

		rr := &types.ReconciliationRequest{
			Instance:  maas,
			Resources: []unstructured.Unstructured{authPolicy},
		}

		err := configureExternalOIDC(t.Context(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// No OIDC rules should be present
		_, found, _ := unstructured.NestedMap(rr.Resources[0].Object,
			"spec", "rules", "authentication", "oidc-identities")
		g.Expect(found).Should(BeFalse())
	})

	t.Run("should succeed silently when maas-api AuthPolicy is not found", func(t *testing.T) {
		maas := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.ModelsAsServiceInstanceName,
			},
			Spec: componentApi.ModelsAsServiceSpec{
				ExternalOIDC: &componentApi.ExternalOIDCConfig{
					IssuerURL: "https://keycloak.example.com/realms/maas",
					ClientID:  "maas-cli",
				},
			},
		}

		// Different AuthPolicy name -- maas-api-auth-policy not present
		otherPolicy := createAuthPolicy("other-policy", "opendatahub", "some-route")

		rr := &types.ReconciliationRequest{
			Instance:  maas,
			Resources: []unstructured.Unstructured{otherPolicy},
		}

		err := configureExternalOIDC(t.Context(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// other-policy should not be modified
		_, found, _ := unstructured.NestedMap(rr.Resources[0].Object,
			"spec", "rules", "authentication", "oidc-identities")
		g.Expect(found).Should(BeFalse())
	})

	t.Run("should only modify maas-api AuthPolicy when multiple resources present", func(t *testing.T) {
		maas := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.ModelsAsServiceInstanceName,
			},
			Spec: componentApi.ModelsAsServiceSpec{
				ExternalOIDC: &componentApi.ExternalOIDCConfig{
					IssuerURL: "https://idp.example.com/realms/test",
					ClientID:  "test-client",
				},
			},
		}

		gatewayPolicy := createAuthPolicy(GatewayAuthPolicyName, "openshift-ingress", "gateway")
		maasAPIPolicy := createAuthPolicy(MaaSAPIAuthPolicyName, "opendatahub", "maas-api-route")

		rr := &types.ReconciliationRequest{
			Instance:  maas,
			Resources: []unstructured.Unstructured{gatewayPolicy, maasAPIPolicy},
		}

		err := configureExternalOIDC(t.Context(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// gateway-auth-policy should NOT have OIDC rules
		_, found, _ := unstructured.NestedMap(rr.Resources[0].Object,
			"spec", "rules", "authentication", "oidc-identities")
		g.Expect(found).Should(BeFalse())

		// maas-api-auth-policy SHOULD have OIDC rules
		issuer, found, err := unstructured.NestedString(rr.Resources[1].Object,
			"spec", "rules", "authentication", "oidc-identities", "jwt", "issuerUrl")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).Should(BeTrue())
		g.Expect(issuer).Should(Equal("https://idp.example.com/realms/test"))
	})

	t.Run("should use default TTL when not specified", func(t *testing.T) {
		maas := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.ModelsAsServiceInstanceName,
			},
			Spec: componentApi.ModelsAsServiceSpec{
				ExternalOIDC: &componentApi.ExternalOIDCConfig{
					IssuerURL: "https://keycloak.example.com/realms/maas",
					ClientID:  "maas-cli",
					TTL:       0,
				},
			},
		}

		authPolicy := createAuthPolicy(MaaSAPIAuthPolicyName, "opendatahub", "maas-api-route")

		rr := &types.ReconciliationRequest{
			Instance:  maas,
			Resources: []unstructured.Unstructured{authPolicy},
		}

		err := configureExternalOIDC(t.Context(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		ttl, found, err := unstructured.NestedFieldNoCopy(rr.Resources[0].Object,
			"spec", "rules", "authentication", "oidc-identities", "jwt", "ttl")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).Should(BeTrue())
		g.Expect(ttl).Should(Equal(int64(300)))
	})
}

func TestValidateExternalOIDCCA(t *testing.T) {
	g := NewWithT(t)

	t.Run("should skip when externalOIDC is nil", func(t *testing.T) {
		maas := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.ModelsAsServiceInstanceName,
			},
			Spec: componentApi.ModelsAsServiceSpec{},
		}

		cli := createFakeClientWithoutGateway()
		rr := &types.ReconciliationRequest{
			Instance: maas,
			Client:   cli,
		}

		err := validateExternalOIDCCA(t.Context(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should skip when caCertificateSecretName is empty", func(t *testing.T) {
		maas := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.ModelsAsServiceInstanceName,
			},
			Spec: componentApi.ModelsAsServiceSpec{
				ExternalOIDC: &componentApi.ExternalOIDCConfig{
					IssuerURL: "https://keycloak.example.com/realms/maas",
					ClientID:  "maas-cli",
				},
			},
		}

		cli := createFakeClientWithoutGateway()
		rr := &types.ReconciliationRequest{
			Instance: maas,
			Client:   cli,
		}

		err := validateExternalOIDCCA(t.Context(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should succeed when Secret exists with valid ca.crt key", func(t *testing.T) {
		maas := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.ModelsAsServiceInstanceName,
			},
			Spec: componentApi.ModelsAsServiceSpec{
				GatewayRef: componentApi.GatewayRef{
					Namespace: "openshift-ingress",
					Name:      "maas-default-gateway",
				},
				ExternalOIDC: &componentApi.ExternalOIDCConfig{
					IssuerURL:               "https://keycloak.example.com/realms/maas",
					ClientID:                "maas-cli",
					CACertificateSecretName: "keycloak-ca",
				},
			},
		}

		caSecret := createCASecret("openshift-ingress", "keycloak-ca",
			"-----BEGIN CERTIFICATE-----\ntest\n-----END CERTIFICATE-----\n")
		cli, err := fakeclient.New(fakeclient.WithObjects(caSecret))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &types.ReconciliationRequest{
			Instance: maas,
			Client:   cli,
		}

		err = validateExternalOIDCCA(t.Context(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should fail when Secret does not exist", func(t *testing.T) {
		maas := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.ModelsAsServiceInstanceName,
			},
			Spec: componentApi.ModelsAsServiceSpec{
				GatewayRef: componentApi.GatewayRef{
					Namespace: "openshift-ingress",
					Name:      "maas-default-gateway",
				},
				ExternalOIDC: &componentApi.ExternalOIDCConfig{
					IssuerURL:               "https://keycloak.example.com/realms/maas",
					ClientID:                "maas-cli",
					CACertificateSecretName: "nonexistent-secret",
				},
			},
		}

		cli, err := fakeclient.New()
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &types.ReconciliationRequest{
			Instance: maas,
			Client:   cli,
		}

		err = validateExternalOIDCCA(t.Context(), rr)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("CA certificate Secret openshift-ingress/nonexistent-secret not found"))
		g.Expect(err.Error()).Should(ContainSubstring(MaaSCACertSecretKey))
	})

	t.Run("should fail when Secret lacks ca.crt key", func(t *testing.T) {
		maas := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.ModelsAsServiceInstanceName,
			},
			Spec: componentApi.ModelsAsServiceSpec{
				GatewayRef: componentApi.GatewayRef{
					Namespace: "openshift-ingress",
					Name:      "maas-default-gateway",
				},
				ExternalOIDC: &componentApi.ExternalOIDCConfig{
					IssuerURL:               "https://keycloak.example.com/realms/maas",
					ClientID:                "maas-cli",
					CACertificateSecretName: "bad-secret",
				},
			},
		}

		badSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bad-secret",
				Namespace: "openshift-ingress",
			},
			Data: map[string][]byte{
				"wrong-key": []byte("some-data"),
			},
		}
		cli, err := fakeclient.New(fakeclient.WithObjects(badSecret))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &types.ReconciliationRequest{
			Instance: maas,
			Client:   cli,
		}

		err = validateExternalOIDCCA(t.Context(), rr)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("does not contain a 'ca.crt' key"))
	})

	t.Run("should fail when ca.crt key is empty", func(t *testing.T) {
		maas := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{
				Name: componentApi.ModelsAsServiceInstanceName,
			},
			Spec: componentApi.ModelsAsServiceSpec{
				GatewayRef: componentApi.GatewayRef{
					Namespace: "openshift-ingress",
					Name:      "maas-default-gateway",
				},
				ExternalOIDC: &componentApi.ExternalOIDCConfig{
					IssuerURL:               "https://keycloak.example.com/realms/maas",
					ClientID:                "maas-cli",
					CACertificateSecretName: "empty-ca-secret",
				},
			},
		}

		emptySecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "empty-ca-secret",
				Namespace: "openshift-ingress",
			},
			Data: map[string][]byte{
				MaaSCACertSecretKey: {},
			},
		}
		cli, err := fakeclient.New(fakeclient.WithObjects(emptySecret))
		g.Expect(err).ShouldNot(HaveOccurred())

		rr := &types.ReconciliationRequest{
			Instance: maas,
			Client:   cli,
		}

		err = validateExternalOIDCCA(t.Context(), rr)
		g.Expect(err).Should(HaveOccurred())
		g.Expect(err.Error()).Should(ContainSubstring("does not contain a 'ca.crt' key with PEM data"))
	})
}

func TestConfigureOIDCCACertificate(t *testing.T) {
	g := NewWithT(t)

	t.Run("should skip when externalOIDC is nil and no Authorino CR found", func(t *testing.T) {
		maas := &componentApi.ModelsAsService{
			ObjectMeta: metav1.ObjectMeta{Name: componentApi.ModelsAsServiceInstanceName},
			Spec:       componentApi.ModelsAsServiceSpec{},
		}

		dsci := createTestDSCI("opendatahub")
		cli, err := fakeclient.New(fakeclient.WithObjects(dsci))
		g.Expect(err).ShouldNot(HaveOccurred())
		dc := newFakeDynamicClient()

		rr := createRRWithDynamic(maas, cli, dc)
		err = configureOIDCCACertificate(t.Context(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should find Authorino CR in kuadrant-system and create ConfigMap there", func(t *testing.T) {
		caCertPEM := "-----BEGIN CERTIFICATE-----\nMIIBtest\n-----END CERTIFICATE-----\n"
		authorinoNS := AuthorinoNamespaceKuadrant

		maas := createTestMaaSWithCA("keycloak-ca")
		caSecret := createCASecret("openshift-ingress", "keycloak-ca", caCertPEM)
		dsci := createTestDSCI("opendatahub")
		authorinoCR := createAuthorinoCR(authorinoNS)

		cli, err := fakeclient.New(fakeclient.WithObjects(caSecret, dsci))
		g.Expect(err).ShouldNot(HaveOccurred())
		dc := newFakeDynamicClient(authorinoCR)

		rr := createRRWithDynamic(maas, cli, dc)
		err = configureOIDCCACertificate(t.Context(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// ConfigMap should be in kuadrant-system
		u, err := dc.Resource(configMapGVR()).Namespace(authorinoNS).Get(
			t.Context(), MaaSCABundleConfigMapName, metav1.GetOptions{})
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(u.Object["data"].(map[string]interface{})[MaaSCABundleFileName]).Should(Equal(caCertPEM))

		cmLabels := u.GetLabels()
		g.Expect(cmLabels[labels.InjectTrustCA]).Should(Equal(labels.True))
		g.Expect(cmLabels[labels.ODH.Component(ComponentName)]).Should(Equal(labels.True))
	})

	t.Run("should find Authorino CR in rh-connectivity-link for RHCL deployments", func(t *testing.T) {
		caCertPEM := "-----BEGIN CERTIFICATE-----\nMIIBtest\n-----END CERTIFICATE-----\n"
		authorinoNS := AuthorinoNamespaceRHCL

		maas := createTestMaaSWithCA("keycloak-ca")
		caSecret := createCASecret("openshift-ingress", "keycloak-ca", caCertPEM)
		dsci := createTestDSCI("redhat-ods-applications")
		authorinoCR := createAuthorinoCR(authorinoNS)

		cli, err := fakeclient.New(fakeclient.WithObjects(caSecret, dsci))
		g.Expect(err).ShouldNot(HaveOccurred())
		dc := newFakeDynamicClient(authorinoCR)

		rr := createRRWithDynamic(maas, cli, dc)
		err = configureOIDCCACertificate(t.Context(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		u, err := dc.Resource(configMapGVR()).Namespace(authorinoNS).Get(
			t.Context(), MaaSCABundleConfigMapName, metav1.GetOptions{})
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(u.Object["data"].(map[string]interface{})[MaaSCABundleFileName]).Should(Equal(caCertPEM))
	})

	t.Run("should add volume entry to Authorino CR spec.volumes.items", func(t *testing.T) {
		caCertPEM := "-----BEGIN CERTIFICATE-----\nMIIBtest\n-----END CERTIFICATE-----\n"
		authorinoNS := AuthorinoNamespaceKuadrant

		maas := createTestMaaSWithCA("keycloak-ca")
		caSecret := createCASecret("openshift-ingress", "keycloak-ca", caCertPEM)
		dsci := createTestDSCI("opendatahub")
		authorinoCR := createAuthorinoCR(authorinoNS)

		cli, err := fakeclient.New(fakeclient.WithObjects(caSecret, dsci))
		g.Expect(err).ShouldNot(HaveOccurred())
		dc := newFakeDynamicClient(authorinoCR)

		rr := createRRWithDynamic(maas, cli, dc)
		err = configureOIDCCACertificate(t.Context(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Read back the Authorino CR and verify spec.volumes.items
		cr, err := dc.Resource(authorinoCRGVR()).Namespace(authorinoNS).Get(
			t.Context(), AuthorinoCRName, metav1.GetOptions{})
		g.Expect(err).ShouldNot(HaveOccurred())

		items, found, err := unstructured.NestedSlice(cr.Object, "spec", "volumes", "items")
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(found).Should(BeTrue())
		g.Expect(items).Should(HaveLen(1))

		vol := items[0].(map[string]interface{})
		g.Expect(vol["name"]).Should(Equal(MaaSCABundleVolumeName))
		g.Expect(vol["mountPath"]).Should(Equal(MaaSCABundleMountPath))
		cms := vol["configMaps"].([]interface{})
		g.Expect(cms).Should(HaveLen(1))
		g.Expect(cms[0]).Should(Equal(MaaSCABundleConfigMapName))
	})

	t.Run("should succeed silently when Authorino CR is not found anywhere", func(t *testing.T) {
		maas := createTestMaaSWithCA("keycloak-ca")
		caSecret := createCASecret("openshift-ingress", "keycloak-ca", "cert-data")
		dsci := createTestDSCI("opendatahub")

		cli, err := fakeclient.New(fakeclient.WithObjects(caSecret, dsci))
		g.Expect(err).ShouldNot(HaveOccurred())
		dc := newFakeDynamicClient()

		rr := createRRWithDynamic(maas, cli, dc)
		err = configureOIDCCACertificate(t.Context(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())
	})

	t.Run("should be idempotent when volume already exists in Authorino CR", func(t *testing.T) {
		caCertPEM := "-----BEGIN CERTIFICATE-----\nMIIBtest\n-----END CERTIFICATE-----\n"
		authorinoNS := AuthorinoNamespaceKuadrant

		maas := createTestMaaSWithCA("keycloak-ca")
		caSecret := createCASecret("openshift-ingress", "keycloak-ca", caCertPEM)
		dsci := createTestDSCI("opendatahub")

		authorinoCR := createAuthorinoCRWithVolume(authorinoNS)

		cli, err := fakeclient.New(fakeclient.WithObjects(caSecret, dsci))
		g.Expect(err).ShouldNot(HaveOccurred())
		dc := newFakeDynamicClient(authorinoCR)

		rr := createRRWithDynamic(maas, cli, dc)
		err = configureOIDCCACertificate(t.Context(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		cr, err := dc.Resource(authorinoCRGVR()).Namespace(authorinoNS).Get(
			t.Context(), AuthorinoCRName, metav1.GetOptions{})
		g.Expect(err).ShouldNot(HaveOccurred())

		items, _, _ := unstructured.NestedSlice(cr.Object, "spec", "volumes", "items")
		caVolumeCount := 0
		for _, item := range items {
			vol := item.(map[string]interface{})
			if vol["name"] == MaaSCABundleVolumeName {
				caVolumeCount++
			}
		}
		g.Expect(caVolumeCount).Should(Equal(1), "Should have exactly one CA bundle volume entry")
	})

	t.Run("should clean up volume from Authorino CR and delete ConfigMap when caCertificateSecretName is cleared", func(t *testing.T) {
		authorinoNS := AuthorinoNamespaceKuadrant

		maas := &componentApi.ModelsAsService{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "components.platform.opendatahub.io/v1alpha1",
				Kind:       "ModelsAsService",
			},
			ObjectMeta: metav1.ObjectMeta{Name: componentApi.ModelsAsServiceInstanceName},
			Spec: componentApi.ModelsAsServiceSpec{
				ExternalOIDC: &componentApi.ExternalOIDCConfig{
					IssuerURL: "https://keycloak.example.com/realms/maas",
					ClientID:  "maas-cli",
				},
			},
		}

		dsci := createTestDSCI("opendatahub")
		cli, err := fakeclient.New(fakeclient.WithObjects(dsci))
		g.Expect(err).ShouldNot(HaveOccurred())

		authorinoCR := createAuthorinoCRWithVolume(authorinoNS)
		existingCM := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":      MaaSCABundleConfigMapName,
					"namespace": authorinoNS,
				},
				"data": map[string]interface{}{
					MaaSCABundleFileName: "old-cert-data",
				},
			},
		}

		dc := newFakeDynamicClientWithObjects(authorinoCR, existingCM)
		rr := createRRWithDynamic(maas, cli, dc)

		err = configureOIDCCACertificate(t.Context(), rr)
		g.Expect(err).ShouldNot(HaveOccurred())

		// Volume should be removed from Authorino CR
		cr, err := dc.Resource(authorinoCRGVR()).Namespace(authorinoNS).Get(
			t.Context(), AuthorinoCRName, metav1.GetOptions{})
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(hasAuthorinoCRVolume(cr)).Should(BeFalse(), "CA volume should be removed from Authorino CR")

		// ConfigMap should be deleted
		_, err = dc.Resource(configMapGVR()).Namespace(authorinoNS).Get(
			t.Context(), MaaSCABundleConfigMapName, metav1.GetOptions{})
		g.Expect(err).Should(HaveOccurred(), "ConfigMap should be deleted during cleanup")
	})
}

// createTestMaaSWithCA creates a ModelsAsService instance with externalOIDC CA config for tests.
func createTestMaaSWithCA(caSecretName string) *componentApi.ModelsAsService {
	return &componentApi.ModelsAsService{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "components.platform.opendatahub.io/v1alpha1",
			Kind:       "ModelsAsService",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.ModelsAsServiceInstanceName,
			UID:  "test-uid-ca",
		},
		Spec: componentApi.ModelsAsServiceSpec{
			GatewayRef: componentApi.GatewayRef{
				Namespace: "openshift-ingress",
				Name:      "maas-default-gateway",
			},
			ExternalOIDC: &componentApi.ExternalOIDCConfig{
				IssuerURL:               "https://keycloak.example.com/realms/maas",
				ClientID:                "maas-cli",
				CACertificateSecretName: caSecretName,
			},
		},
	}
}

// createCASecret creates a Secret object containing a CA certificate.
func createCASecret(namespace, name, caCert string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			MaaSCACertSecretKey: []byte(caCert),
		},
	}
}

// createTestDSCI creates a DSCInitialization resource for testing.
func createTestDSCI(appNamespace string) *dsciv2.DSCInitialization {
	dsci := &dsciv2.DSCInitialization{}
	dsci.SetGroupVersionKind(gvk.DSCInitialization)
	dsci.SetName("default-dsci")
	dsci.Spec.ApplicationsNamespace = appNamespace
	return dsci
}

// createAuthorinoCR creates an Authorino CR (operator.authorino.kuadrant.io/v1beta1)
// matching real-world topology: name "authorino" in the given namespace.
func createAuthorinoCR(namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "operator.authorino.kuadrant.io/v1beta1",
			"kind":       "Authorino",
			"metadata": map[string]interface{}{
				"name":      AuthorinoCRName,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"listener": map[string]interface{}{
					"tls": map[string]interface{}{"enabled": false},
				},
				"oidcServer": map[string]interface{}{
					"tls": map[string]interface{}{"enabled": false},
				},
			},
		},
	}
}

// createAuthorinoCRWithVolume creates an Authorino CR that already has the CA bundle
// volume configured (for idempotency and cleanup tests).
func createAuthorinoCRWithVolume(namespace string) *unstructured.Unstructured {
	cr := createAuthorinoCR(namespace)
	_ = unstructured.SetNestedSlice(cr.Object, []interface{}{
		map[string]interface{}{
			"name":       MaaSCABundleVolumeName,
			"mountPath":  MaaSCABundleMountPath,
			"configMaps": []interface{}{MaaSCABundleConfigMapName},
		},
	}, "spec", "volumes", "items")
	return cr
}

// newFakeDynamicClient creates a fake dynamic client with optional unstructured objects.
func newFakeDynamicClient(objects ...*unstructured.Unstructured) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	runtimeObjs := make([]runtime.Object, 0, len(objects))
	for _, o := range objects {
		runtimeObjs = append(runtimeObjs, o)
	}

	return dynamicfake.NewSimpleDynamicClient(scheme, runtimeObjs...)
}

// newFakeDynamicClientWithObjects creates a fake dynamic client with multiple unstructured objects.
func newFakeDynamicClientWithObjects(objects ...*unstructured.Unstructured) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	runtimeObjs := make([]runtime.Object, 0, len(objects))
	for _, o := range objects {
		runtimeObjs = append(runtimeObjs, o)
	}

	return dynamicfake.NewSimpleDynamicClient(scheme, runtimeObjs...)
}

// createRRWithDynamic creates a ReconciliationRequest with a MockController wired to the
// provided dynamic client, so cross-namespace lookups bypass the cache.
func createRRWithDynamic(
	maas *componentApi.ModelsAsService,
	cli client.Client,
	dc *dynamicfake.FakeDynamicClient,
) *types.ReconciliationRequest {
	ctrl := mocks.NewMockController(func(m *mocks.MockController) {
		m.On("GetDynamicClient").Return(dc)
	})

	return &types.ReconciliationRequest{
		Instance:   maas,
		Client:     cli,
		Controller: ctrl,
	}
}
