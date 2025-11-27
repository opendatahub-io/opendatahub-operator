//nolint:testpackage
package gateway

import (
	"context"
	"testing"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

// TestCreateNetworkPolicy tests the createNetworkPolicy function with various configurations.
func TestCreateNetworkPolicy(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	testCases := []struct {
		name           string
		config         *serviceApi.NetworkPolicyConfig
		expectTemplate bool
		expectError    bool
		description    string
	}{
		{
			name:           "nil config enables ingress by default",
			config:         nil,
			expectTemplate: true,
			expectError:    false,
			description:    "should add NetworkPolicy template when config is nil (default enabled)",
		},
		{
			name: "nil ingress enables ingress by default",
			config: &serviceApi.NetworkPolicyConfig{
				Ingress: nil,
			},
			expectTemplate: true,
			expectError:    false,
			description:    "should add NetworkPolicy template when Ingress is nil (default enabled)",
		},
		{
			name: "ingress enabled explicitly",
			config: &serviceApi.NetworkPolicyConfig{
				Ingress: &serviceApi.IngressPolicyConfig{
					Enabled: true,
				},
			},
			expectTemplate: true,
			expectError:    false,
			description:    "should add NetworkPolicy template when ingress is explicitly enabled",
		},
		{
			name: "ingress disabled explicitly",
			config: &serviceApi.NetworkPolicyConfig{
				Ingress: &serviceApi.IngressPolicyConfig{
					Enabled: false,
				},
			},
			expectTemplate: false,
			expectError:    false,
			description:    "should not add NetworkPolicy template when ingress is explicitly disabled",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			rr := &odhtypes.ReconciliationRequest{
				Client:    setupTestClient(),
				Templates: []odhtypes.TemplateInfo{},
			}

			initialTemplateCount := len(rr.Templates)

			err := createNetworkPolicy(ctx, rr, tc.config)

			if tc.expectError {
				g.Expect(err).To(HaveOccurred(), tc.description)
			} else {
				g.Expect(err).NotTo(HaveOccurred(), tc.description)

				if tc.expectTemplate {
					g.Expect(rr.Templates).To(HaveLen(initialTemplateCount+1), "should add one template")
					template := rr.Templates[len(rr.Templates)-1]
					g.Expect(template.Path).To(Equal(NetworkPolicyTemplate), "template path should match")
					g.Expect(template.FS).To(Equal(gatewayResources), "template FS should match gatewayResources")
				} else {
					g.Expect(rr.Templates).To(HaveLen(initialTemplateCount), "should not add any template")
				}
			}
		})
	}
}

// TestCreateNetworkPolicyTemplateValidation tests that createNetworkPolicy validates template file exists.
func TestCreateNetworkPolicyTemplateValidation(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	ctx := context.Background()
	rr := &odhtypes.ReconciliationRequest{
		Client:    setupTestClient(),
		Templates: []odhtypes.TemplateInfo{},
	}

	// This test verifies that the template file exists check works
	// Since the template file should exist in the embedded FS, this should succeed
	config := &serviceApi.NetworkPolicyConfig{
		Ingress: &serviceApi.IngressPolicyConfig{
			Enabled: true,
		},
	}

	err := createNetworkPolicy(ctx, rr, config)
	g.Expect(err).NotTo(HaveOccurred(), "should not error when template file exists")
	g.Expect(rr.Templates).To(HaveLen(1), "should add template when file exists")
}

// TestGetNetworkPolicyTemplateData tests the getNetworkPolicyTemplateData function.
func TestGetNetworkPolicyTemplateData(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	ctx := context.Background()
	rr := &odhtypes.ReconciliationRequest{
		Client: setupTestClient(),
	}

	data, err := getNetworkPolicyTemplateData(ctx, rr)

	g.Expect(err).NotTo(HaveOccurred(), "should not return error")
	g.Expect(data).NotTo(BeNil(), "should return non-nil data map")

	// Verify all expected keys are present
	expectedKeys := []string{"NetworkPolicyName", "NetworkPolicyNamespace", "GatewayName", "HTTPSPort", "MetricsPort"}
	for _, key := range expectedKeys {
		g.Expect(data).To(HaveKey(key), "should contain key: %s", key)
	}

	// Verify values match expected constants
	g.Expect(data["NetworkPolicyName"]).To(Equal(KubeAuthProxyName), "NetworkPolicyName should match KubeAuthProxyName")
	g.Expect(data["NetworkPolicyNamespace"]).To(Equal(GatewayNamespace), "NetworkPolicyNamespace should match GatewayNamespace")
	g.Expect(data["GatewayName"]).To(Equal(DefaultGatewayName), "GatewayName should match DefaultGatewayName")
	g.Expect(data["HTTPSPort"]).To(Equal(AuthProxyHTTPSPort), "HTTPSPort should match AuthProxyHTTPSPort")
	g.Expect(data["MetricsPort"]).To(Equal(AuthProxyMetricsPort), "MetricsPort should match AuthProxyMetricsPort")
}

// TestGetNetworkPolicyTemplateDataWithNilRequest tests that getNetworkPolicyTemplateData handles nil request.
func TestGetNetworkPolicyTemplateDataWithNilRequest(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	ctx := context.Background()
	data, err := getNetworkPolicyTemplateData(ctx, nil)

	g.Expect(err).NotTo(HaveOccurred(), "should not return error even with nil request")
	g.Expect(data).NotTo(BeNil(), "should return non-nil data map")
	g.Expect(data).To(HaveKey("NetworkPolicyName"), "should contain NetworkPolicyName key")
}
