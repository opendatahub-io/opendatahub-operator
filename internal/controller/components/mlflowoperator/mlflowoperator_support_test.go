/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package mlflowoperator_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/mlflowoperator"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestComputeKustomizeVariable(t *testing.T) {
	t.Parallel() // Enable parallel execution for better performance

	// Define test constants for better maintainability
	const (
		defaultDomain = "apps.example.com"
		customDomain  = "custom.domain.com"
	)

	// Pre-create reusable gateway configs to avoid repeated allocations
	var (
		customGatewayConfig = func() *serviceApi.GatewayConfig {
			gc := &serviceApi.GatewayConfig{}
			gc.SetName(serviceApi.GatewayConfigName)
			gc.Spec.Domain = customDomain
			return gc
		}
		defaultGatewayConfig = func() *serviceApi.GatewayConfig {
			gc := &serviceApi.GatewayConfig{}
			gc.SetName(serviceApi.GatewayConfigName)
			// No custom domain, should use cluster domain
			return gc
		}
	)

	tests := []struct {
		name              string
		platform          common.Platform
		expectedURL       string
		expectedTitle     string
		gatewayConfigFunc func() *serviceApi.GatewayConfig
		clusterDomain     string
		expectError       bool
	}{
		{
			name:              "OpenDataHub platform with default domain",
			platform:          cluster.OpenDataHub,
			expectedURL:       "https://data-science-gateway." + defaultDomain + "/",
			expectedTitle:     "OpenShift Open Data Hub",
			gatewayConfigFunc: defaultGatewayConfig, // Use default GatewayConfig (no custom domain)
			clusterDomain:     defaultDomain,
		},
		{
			name:              "RHOAI platform with custom domain",
			platform:          cluster.SelfManagedRhoai,
			expectedURL:       "https://data-science-gateway." + customDomain + "/",
			expectedTitle:     "OpenShift Self Managed Services",
			gatewayConfigFunc: customGatewayConfig,
			clusterDomain:     defaultDomain, // Should be ignored due to custom domain
		},
	}

	for _, tt := range tests {
		// Capture loop variable for parallel execution

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t) // Each subtest needs its own Gomega instance
			ctx := t.Context()

			// Pre-allocate slice with known capacity for better performance
			objects := make([]client.Object, 0, 2)

			if gc := tt.gatewayConfigFunc(); gc != nil {
				objects = append(objects, gc)
			}

			// Mock cluster domain by creating a fake OpenShift Ingress object
			if tt.clusterDomain != "" {
				ingress := createMockOpenShiftIngress(tt.clusterDomain)
				objects = append(objects, ingress)
			}

			cli, err := fakeclient.New(fakeclient.WithObjects(objects...))
			g.Expect(err).ShouldNot(HaveOccurred())

			result, err := mlflowoperator.ComputeKustomizeVariable(ctx, cli, tt.platform)

			if tt.expectError {
				g.Expect(err).Should(HaveOccurred())
				return
			}

			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(result).Should(HaveKeyWithValue("mlflow-url", tt.expectedURL))
			g.Expect(result).Should(HaveKeyWithValue("section-title", tt.expectedTitle))
		})
	}
}

func TestComputeKustomizeVariableError(t *testing.T) {
	t.Parallel() // Enable parallel execution for better performance
	g := NewWithT(t)
	ctx := t.Context()

	// Create a client with no objects to simulate GatewayConfig not found
	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	// Test error handling with better error message validation
	_, err = mlflowoperator.ComputeKustomizeVariable(ctx, cli, cluster.OpenDataHub)
	g.Expect(err).Should(HaveOccurred(), "Should fail when cluster domain cannot be determined")
	g.Expect(err.Error()).Should(ContainSubstring("error getting gateway domain"), "Error should contain expected message")
}

// createMockOpenShiftIngress creates an optimized mock OpenShift Ingress object
// for testing cluster domain resolution.
func createMockOpenShiftIngress(domain string) client.Object {
	// Input validation for better error handling
	if domain == "" {
		domain = "default.example.com" // Fallback domain
	}

	// Create OpenShift Ingress object (config.openshift.io/v1/Ingress)
	// that cluster.GetDomain() looks for
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "config.openshift.io/v1",
			"kind":       "Ingress",
			"metadata": map[string]interface{}{
				"name": "cluster",
			},
			"spec": map[string]interface{}{
				"domain": domain,
			},
		},
	}

	return obj
}
