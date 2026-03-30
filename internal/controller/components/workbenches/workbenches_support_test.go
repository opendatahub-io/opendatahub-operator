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

package workbenches_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/workbenches"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestComputeKustomizeVariable(t *testing.T) {
	t.Parallel()

	const (
		defaultDomain = "apps.example.com"
		customDomain  = "custom.domain.com"
	)

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
			return gc
		}
	)

	tests := []struct {
		name                  string
		platform              common.Platform
		expectedURL           string
		expectedTitle         string
		expectedMLflowEnabled string
		gatewayConfigFunc     func() *serviceApi.GatewayConfig
		mlflowManagementState operatorv1.ManagementState
		clusterDomain         string
		expectError           bool
	}{
		{
			name:                  "OpenDataHub platform with default domain and MLflow managed",
			platform:              cluster.OpenDataHub,
			expectedURL:           gateway.DefaultGatewaySubdomain + "." + defaultDomain,
			expectedTitle:         "OpenShift Open Data Hub",
			expectedMLflowEnabled: "true",
			gatewayConfigFunc:     defaultGatewayConfig,
			mlflowManagementState: operatorv1.Managed,
			clusterDomain:         defaultDomain,
		},
		{
			name:                  "RHOAI platform with custom domain and MLflow removed",
			platform:              cluster.SelfManagedRhoai,
			expectedURL:           gateway.DefaultGatewaySubdomain + "." + customDomain,
			expectedTitle:         "OpenShift Self Managed Services",
			expectedMLflowEnabled: "false",
			gatewayConfigFunc:     customGatewayConfig,
			mlflowManagementState: operatorv1.Removed,
			clusterDomain:         defaultDomain,
		},
		{
			name:                  "ManagedRhoai platform with custom domain and MLflow managed",
			platform:              cluster.ManagedRhoai,
			expectedURL:           gateway.DefaultGatewaySubdomain + "." + customDomain,
			expectedTitle:         "OpenShift Managed Services",
			expectedMLflowEnabled: "true",
			gatewayConfigFunc:     customGatewayConfig,
			mlflowManagementState: operatorv1.Managed,
			clusterDomain:         defaultDomain,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			ctx := t.Context()

			objects := make([]client.Object, 0, 3)

			if gc := tt.gatewayConfigFunc(); gc != nil {
				objects = append(objects, gc)
			}

			if tt.clusterDomain != "" {
				ingress := createMockOpenShiftIngress(tt.clusterDomain)
				objects = append(objects, ingress)
			}

			dsc := createMockDSC(tt.mlflowManagementState)
			objects = append(objects, dsc)

			cli, err := fakeclient.New(fakeclient.WithObjects(objects...))
			g.Expect(err).ShouldNot(HaveOccurred())

			result, err := workbenches.ComputeKustomizeVariable(ctx, cli, tt.platform)

			if tt.expectError {
				g.Expect(err).Should(HaveOccurred())
				return
			}

			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(result).Should(HaveKeyWithValue("gateway-url", tt.expectedURL))
			g.Expect(result).Should(HaveKeyWithValue("section-title", tt.expectedTitle))
			g.Expect(result).Should(HaveKeyWithValue("mlflow-enabled", tt.expectedMLflowEnabled))
		})
	}
}

func TestComputeKustomizeVariableError(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	_, err = workbenches.ComputeKustomizeVariable(ctx, cli, cluster.OpenDataHub)
	g.Expect(err).Should(HaveOccurred(), "Should fail when cluster domain cannot be determined")
	g.Expect(err.Error()).Should(ContainSubstring("error getting gateway domain"), "Error should contain expected message")
}

func TestComputeKustomizeVariableEmptyDomain(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()

	// Create a Gateway CR with an empty hostname to simulate empty domain scenario
	emptyHostname := gwapiv1.Hostname("")
	gw := &gwapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gateway.DefaultGatewayName,
			Namespace: gateway.GatewayNamespace,
		},
		Spec: gwapiv1.GatewaySpec{
			Listeners: []gwapiv1.Listener{
				{
					Hostname: &emptyHostname,
				},
			},
		},
	}

	gc := &serviceApi.GatewayConfig{}
	gc.SetName(serviceApi.GatewayConfigName)

	dsc := createMockDSC(operatorv1.Managed)

	cli, err := fakeclient.New(fakeclient.WithObjects(gw, gc, dsc))
	g.Expect(err).ShouldNot(HaveOccurred())

	result, err := workbenches.ComputeKustomizeVariable(ctx, cli, cluster.OpenDataHub)
	g.Expect(err).ShouldNot(HaveOccurred(), "Empty domain should not cause error")
	g.Expect(result).Should(HaveKeyWithValue("gateway-url", ""),
		"Empty domain should result in empty gateway-url")
	g.Expect(result).Should(HaveKeyWithValue("mlflow-enabled", "true"),
		"MLflow should still be computed correctly")
	g.Expect(result).Should(HaveKeyWithValue("section-title", "OpenShift Open Data Hub"),
		"Section title should still be computed correctly")
}

func TestComputeKustomizeVariableInvalidDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		domain string
	}{
		{
			name:   "domain with newline",
			domain: "evil.com\nmalicious-key=value",
		},
		{
			name:   "domain with carriage return",
			domain: "evil.com\rmalicious-key=value",
		},
		{
			name:   "domain with equals sign",
			domain: "evil.com=malicious",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			ctx := t.Context()

			gc := &serviceApi.GatewayConfig{}
			gc.SetName(serviceApi.GatewayConfigName)
			gc.Spec.Domain = tt.domain

			dsc := createMockDSC(operatorv1.Managed)

			cli, err := fakeclient.New(fakeclient.WithObjects(gc, dsc))
			g.Expect(err).ShouldNot(HaveOccurred())

			_, err = workbenches.ComputeKustomizeVariable(ctx, cli, cluster.OpenDataHub)
			g.Expect(err).Should(HaveOccurred(), "Should reject domain with illegal characters")
			g.Expect(err.Error()).Should(ContainSubstring("invalid gateway domain"))
			g.Expect(err.Error()).Should(ContainSubstring("contains illegal characters"))
		})
	}
}

func TestComputeKustomizeVariableUnknownPlatformFallback(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()

	const defaultDomain = "apps.example.com"

	gc := &serviceApi.GatewayConfig{}
	gc.SetName(serviceApi.GatewayConfigName)

	ingress := createMockOpenShiftIngress(defaultDomain)
	dsc := createMockDSC(operatorv1.Managed)

	cli, err := fakeclient.New(fakeclient.WithObjects(gc, ingress, dsc))
	g.Expect(err).ShouldNot(HaveOccurred())

	unknownPlatform := common.Platform("unknown-platform")
	result, err := workbenches.ComputeKustomizeVariable(ctx, cli, unknownPlatform)
	g.Expect(err).ShouldNot(HaveOccurred(), "Unknown platform should use fallback")
	g.Expect(result).Should(HaveKeyWithValue("section-title", "OpenShift Self Managed Services"),
		"Unknown platform should fallback to SelfManagedRhoai section title")
}

func createMockOpenShiftIngress(domain string) client.Object {
	if domain == "" {
		domain = "default.example.com"
	}

	obj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "config.openshift.io/v1",
			"kind":       "Ingress",
			"metadata": map[string]any{
				"name": "cluster",
			},
			"spec": map[string]any{
				"domain": domain,
			},
		},
	}

	return obj
}

func createMockDSC(mlflowState operatorv1.ManagementState) *dscv2.DataScienceCluster {
	dsc := &dscv2.DataScienceCluster{}
	dsc.SetName("default-dsc")
	dsc.Spec.Components.MLflowOperator.ManagementState = mlflowState
	return dsc
}
