//nolint:testpackage,dupl
package dashboard

import (
	"context"
	"encoding/json"
	"testing"

	gt "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func TestGetName(t *testing.T) {
	g := NewWithT(t)
	handler := &componentHandler{}

	name := handler.GetName()
	g.Expect(name).Should(Equal(componentApi.DashboardComponentName))
}

func TestNewCRObject(t *testing.T) {
	handler := &componentHandler{}

	g := NewWithT(t)
	dsc := createDSCWithDashboard(operatorv1.Managed)
	gatewayConfig := &serviceApi.GatewayConfig{}
	gatewayConfig.SetName(serviceApi.GatewayConfigName)
	gatewayConfig.Status.Domain = "gateway.example.com"
	cl, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig))
	g.Expect(err).To(Succeed())

	cr, err := handler.NewCRObject(context.Background(), cl, dsc)
	g.Expect(err).To(Succeed())
	g.Expect(cr).ShouldNot(BeNil())
	g.Expect(cr).Should(BeAssignableToTypeOf(&componentApi.Dashboard{}))

	g.Expect(cr).Should(WithTransform(json.Marshal, And(
		jq.Match(`.metadata.name == "%s"`, componentApi.DashboardInstanceName),
		jq.Match(`.kind == "%s"`, componentApi.DashboardKind),
		jq.Match(`.apiVersion == "%s"`, componentApi.GroupVersion),
		jq.Match(`.metadata.annotations["%s"] == "%s"`, annotations.ManagementStateAnnotation, operatorv1.Managed),
	)))
}

func TestNewCRObject_ReturnsError_WhenGatewayDomainMissing(t *testing.T) {
	handler := &componentHandler{}
	g := NewWithT(t)
	dsc := createDSCWithDashboard(operatorv1.Managed)

	t.Run("returns error when GatewayConfig does not exist", func(t *testing.T) {
		cl, err := fakeclient.New()
		g.Expect(err).To(Succeed())

		cr, err := handler.NewCRObject(context.Background(), cl, dsc)
		g.Expect(err).To(HaveOccurred())
		g.Expect(cr).To(BeNil())
		g.Expect(err.Error()).To(ContainSubstring("gateway domain is missing for Dashboard"))
		g.Expect(err.Error()).To(ContainSubstring("GatewayConfig"))
	})

	t.Run("returns error when GatewayConfig exists but Status.Domain is empty", func(t *testing.T) {
		gatewayConfig := &serviceApi.GatewayConfig{}
		gatewayConfig.SetName(serviceApi.GatewayConfigName)
		// Status.Domain left empty
		cl, err := fakeclient.New(fakeclient.WithObjects(gatewayConfig))
		g.Expect(err).To(Succeed())

		cr, err := handler.NewCRObject(context.Background(), cl, dsc)
		g.Expect(err).To(HaveOccurred())
		g.Expect(cr).To(BeNil())
		g.Expect(err.Error()).To(ContainSubstring("gateway domain is missing for Dashboard"))
		g.Expect(err.Error()).To(ContainSubstring("GatewayConfig"))
	})
}

func TestIsEnabled(t *testing.T) {
	handler := &componentHandler{}

	tests := []struct {
		name    string
		state   operatorv1.ManagementState
		matcher gt.GomegaMatcher
	}{
		{
			name:    "should return true when management state is Managed",
			state:   operatorv1.Managed,
			matcher: BeTrue(),
		},
		{
			name:    "should return false when management state is Removed",
			state:   operatorv1.Removed,
			matcher: BeFalse(),
		},
		{
			name:    "should return false when management state is Unmanaged",
			state:   operatorv1.Unmanaged,
			matcher: BeFalse(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			dsc := createDSCWithDashboard(tt.state)

			g.Expect(
				handler.IsEnabled(dsc),
			).Should(
				tt.matcher,
			)
		})
	}
}

func TestUpdateDSCStatus(t *testing.T) {
	handler := &componentHandler{}

	t.Run("should handle enabled component with ready Dashboard CR", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithDashboard(operatorv1.Managed)
		dashboard := createDashboardCR(true)

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dashboard))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &odhtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionTrue))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.components.dashboard.managementState == "%s"`, operatorv1.Managed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.ReadyReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "Component is ready"`, ReadyConditionType)),
		))
	})

	t.Run("should handle enabled component with not ready Dashboard CR", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithDashboard(operatorv1.Managed)
		dashboard := createDashboardCR(false)

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dashboard))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &odhtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionFalse))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.components.dashboard.managementState == "%s"`, operatorv1.Managed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, status.NotReadyReason),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "Component is not ready"`, ReadyConditionType)),
		))
	})

	t.Run("should handle disabled component", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithDashboard(operatorv1.Removed)

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &odhtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionUnknown))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.components.dashboard.managementState == "%s"`, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("Component ManagementState is set to Removed")`, ReadyConditionType)),
		))
	})

	t.Run("should handle empty management state as Removed", func(t *testing.T) {
		g := NewWithT(t)
		ctx := t.Context()

		dsc := createDSCWithDashboard("")

		cli, err := fakeclient.New(fakeclient.WithObjects(dsc))
		g.Expect(err).ShouldNot(HaveOccurred())

		cs, err := handler.UpdateDSCStatus(ctx, &odhtypes.ReconciliationRequest{
			Client:     cli,
			Instance:   dsc,
			Conditions: conditions.NewManager(dsc, ReadyConditionType),
		})

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(cs).Should(Equal(metav1.ConditionUnknown))

		g.Expect(dsc).Should(WithTransform(json.Marshal, And(
			jq.Match(`.status.components.dashboard.managementState == "%s"`, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, ReadyConditionType, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, ReadyConditionType, operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .severity == "%s"`, ReadyConditionType, common.ConditionSeverityInfo),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("Component ManagementState is set to Removed")`, ReadyConditionType)),
		))
	})
}

func TestComputeKustomizeVariable(t *testing.T) {
	t.Parallel()     // Enable parallel execution for better performance
	g := NewWithT(t) // Create once outside the loop for better performance

	// Define test constants for better maintainability
	const (
		defaultDomain = "apps.example.com"
		customDomain  = "custom.domain.com"
		managedDomain = "apps.managed.com"
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
			expectedURL:       "https://" + gateway.DefaultGatewaySubdomain + "." + defaultDomain + "/",
			expectedTitle:     "OpenShift Open Data Hub",
			gatewayConfigFunc: defaultGatewayConfig, // Use default GatewayConfig (no custom domain)
			clusterDomain:     defaultDomain,
		},
		{
			name:              "RHOAI platform with custom domain",
			platform:          cluster.SelfManagedRhoai,
			expectedURL:       "https://" + gateway.DefaultGatewaySubdomain + "." + customDomain + "/",
			expectedTitle:     "OpenShift Self Managed Services",
			gatewayConfigFunc: customGatewayConfig,
			clusterDomain:     defaultDomain, // Should be ignored due to custom domain
		},
		{
			name:              "Managed RHOAI platform with default domain",
			platform:          cluster.ManagedRhoai,
			expectedURL:       "https://" + gateway.DefaultGatewaySubdomain + "." + managedDomain + "/",
			expectedTitle:     "OpenShift Managed Services",
			gatewayConfigFunc: defaultGatewayConfig,
			clusterDomain:     managedDomain,
		},
	}

	for _, tt := range tests {
		// Capture loop variable for parallel execution

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

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

			// Calculate expected domain based on test case
			// This simulates what syncDomainFromGateway would do in real reconciliation
			var expectedDomain string
			if gc := tt.gatewayConfigFunc(); gc != nil {
				if gc.Spec.Domain != "" {
					subdomain := gc.Spec.Subdomain
					if subdomain == "" {
						subdomain = gateway.DefaultGatewaySubdomain
					}
					expectedDomain = subdomain + "." + gc.Spec.Domain
				} else {
					expectedDomain = gateway.DefaultGatewaySubdomain + "." + tt.clusterDomain
				}
			} else {
				expectedDomain = gateway.DefaultGatewaySubdomain + "." + tt.clusterDomain
			}

			// Create Dashboard instance with gateway domain pre-populated (as DSC controller would do)
			dashboard := &componentApi.Dashboard{
				Spec: componentApi.DashboardSpec{
					Gateway: &common.GatewaySpec{
						Domain: expectedDomain,
					},
				},
			}
			rr := &odhtypes.ReconciliationRequest{
				Client:   cli,
				Instance: dashboard,
			}

			result, err := computeKustomizeVariable(rr, tt.platform)

			if tt.expectError {
				g.Expect(err).Should(HaveOccurred())
				return
			}

			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(result).Should(HaveKeyWithValue("dashboard-url", tt.expectedURL))
			g.Expect(result).Should(HaveKeyWithValue("section-title", tt.expectedTitle))
			g.Expect(result).Should(HaveKey("gateway-domain"))
		})
	}
}

func TestComputeKustomizeVariableError(t *testing.T) {
	t.Parallel() // Enable parallel execution for better performance
	g := NewWithT(t)

	// Create a client with no objects to simulate GatewayConfig not found
	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	// Create Dashboard instance with Gateway but empty Domain (tests runtime validation)
	dashboard := &componentApi.Dashboard{
		Spec: componentApi.DashboardSpec{
			Gateway: &common.GatewaySpec{Domain: ""},
		},
	}
	rr := &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dashboard,
	}

	// Test error handling - should fail fast when Spec.Gateway.Domain is empty
	_, err = computeKustomizeVariable(rr, cluster.OpenDataHub)
	g.Expect(err).Should(HaveOccurred(), "Should fail when dashboard.Spec.Gateway.Domain is empty")
	g.Expect(err.Error()).Should(ContainSubstring("gateway domain is missing for Dashboard"), "Error should contain expected message")
}

func createDSCWithDashboard(managementState operatorv1.ManagementState) *dscv2.DataScienceCluster {
	dsc := dscv2.DataScienceCluster{}
	dsc.SetGroupVersionKind(gvk.DataScienceCluster)
	dsc.SetName("test-dsc")

	dsc.Spec.Components.Dashboard.ManagementState = managementState

	return &dsc
}

func createDashboardCR(ready bool) *componentApi.Dashboard {
	c := componentApi.Dashboard{}
	c.SetGroupVersionKind(gvk.Dashboard)
	c.SetName(componentApi.DashboardInstanceName)
	c.Spec.Gateway = &common.GatewaySpec{Domain: "test.example.com"}

	if ready {
		c.Status.Conditions = []common.Condition{{
			Type:    status.ConditionTypeReady,
			Status:  metav1.ConditionTrue,
			Reason:  status.ReadyReason,
			Message: "Component is ready",
		}}
	} else {
		c.Status.Conditions = []common.Condition{{
			Type:    status.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  status.NotReadyReason,
			Message: "Component is not ready",
		}}
	}

	return &c
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
