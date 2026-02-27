package e2e_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func (tc *GatewayTestCtx) DashboardRedirectTestSuite(t *testing.T) {
	t.Helper()

	skipUnless(t, []TestTag{Tier1})

	// Skip all tests if not in OcpRoute mode
	if !tc.isOcpRouteMode(t) {
		t.Skip("Dashboard redirects are only created in OcpRoute ingress mode")
	}

	testCases := []TestCase{
		{"Validate dashboard redirect ConfigMap", tc.ValidateDashboardRedirectConfigMap},
		{"Validate dashboard redirect Deployment", tc.ValidateDashboardRedirectDeployment},
		{"Validate dashboard redirect Service", tc.ValidateDashboardRedirectService},
		{"Validate dashboard redirect Routes", tc.ValidateDashboardRedirectRoutes},
		{"Validate dashboard redirect HTTP functionality", tc.ValidateDashboardRedirectHTTP},
	}

	RunTestCases(t, testCases)
}

// ValidateDashboardRedirectConfigMap validates the nginx configuration for redirects.
func (tc *GatewayTestCtx) ValidateDashboardRedirectConfigMap(t *testing.T) {
	t.Helper()
	t.Log("Validating dashboard redirect ConfigMap")

	skipUnless(t, []TestTag{Tier1})

	appNamespace := tc.AppsNamespace
	expectedGatewayHostname := tc.getExpectedGatewayHostname(t)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ConfigMap, types.NamespacedName{
			Name:      gateway.DashboardRedirectConfigName,
			Namespace: appNamespace,
		}),
		WithCondition(And(
			// Labels
			jq.Match(`.metadata.labels.app == "%s"`, gateway.DashboardRedirectName),
			jq.Match(`.metadata.labels["%s"] == "%s"`, labels.PlatformPartOf, gateway.PartOfGatewayConfig),

			// Owner reference to GatewayConfig
			jq.Match(`.metadata.ownerReferences | length > 0`),
			jq.Match(`.metadata.ownerReferences[] | select(.kind == "GatewayConfig") | .controller == true`),

			// Nginx config content
			jq.Match(`.data."redirect.conf" != null`),
			jq.Match(`.data."redirect.conf" | contains("location /")`),
			jq.Match(`.data."redirect.conf" | contains("return 301 https://%s")`, expectedGatewayHostname),
			jq.Match(`.data."redirect.conf" | contains("$request_uri")`),

			// Should NOT contain server block (location block only)
			jq.Match(`.data."redirect.conf" | contains("server {") | not`),
		)),
		WithCustomErrorMsg("dashboard-redirect ConfigMap should exist with correct nginx location block redirecting to %s", expectedGatewayHostname),
	)

	t.Log("Dashboard redirect ConfigMap validation completed")
}

// ValidateDashboardRedirectDeployment validates the nginx deployment configuration.
//
// This test verifies critical deployment details discovered during implementation:
// - S2I command is required for UBI nginx image
// - ConfigMap must be mounted at specific path for nginx.default.d
// - 2 replicas for high availability
// - Proper security context and resource limits.
func (tc *GatewayTestCtx) ValidateDashboardRedirectDeployment(t *testing.T) {
	t.Helper()
	skipUnless(t, []TestTag{Tier1})
	t.Log("Validating dashboard redirect Deployment")

	appNamespace := tc.AppsNamespace

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      gateway.DashboardRedirectName,
			Namespace: appNamespace,
		}),
		WithCondition(And(
			// Basic deployment config
			jq.Match(`.spec.replicas == 2`),
			jq.Match(`.spec.selector.matchLabels.app == "%s"`, gateway.DashboardRedirectName),

			// Labels
			jq.Match(`.metadata.labels.app == "%s"`, gateway.DashboardRedirectName),
			jq.Match(`.metadata.labels["%s"] == "%s"`, labels.PlatformPartOf, gateway.PartOfGatewayConfig),
			jq.Match(`.spec.template.metadata.labels.app == "%s"`, gateway.DashboardRedirectName),

			// Owner reference
			jq.Match(`.metadata.ownerReferences[] | select(.kind == "GatewayConfig") | .controller == true`),

			// Container config
			jq.Match(`.spec.template.spec.containers | length == 1`),
			jq.Match(`.spec.template.spec.containers[0].name == "nginx"`),
			jq.Match(`.spec.template.spec.containers[0].image | contains("nginx-126")`),

			// Critical: S2I command must be present
			jq.Match(`.spec.template.spec.containers[0].command | length == 1`),
			jq.Match(`.spec.template.spec.containers[0].command[0] == "/usr/libexec/s2i/run"`),

			// Ports
			jq.Match(`.spec.template.spec.containers[0].ports[] | select(.name == "http") | .containerPort == 8080`),

			// Volume mounts - ConfigMap must be mounted at specific path
			jq.Match(`.spec.template.spec.containers[0].volumeMounts[] | select(.name == "redirect-config") | .mountPath == "/opt/app-root/etc/nginx.default.d/redirect.conf"`),
			jq.Match(`.spec.template.spec.containers[0].volumeMounts[] | select(.name == "redirect-config") | .subPath == "redirect.conf"`),

			// Volumes
			jq.Match(`.spec.template.spec.volumes[] | select(.name == "redirect-config") | .configMap.name == "%s"`, gateway.DashboardRedirectConfigName),

			// Resources
			jq.Match(`.spec.template.spec.containers[0].resources.requests.cpu == "50m"`),
			jq.Match(`.spec.template.spec.containers[0].resources.requests.memory == "64Mi"`),
			jq.Match(`.spec.template.spec.containers[0].resources.limits.cpu == "200m"`),
			jq.Match(`.spec.template.spec.containers[0].resources.limits.memory == "128Mi"`),

			// Security context
			jq.Match(`.spec.template.spec.containers[0].securityContext.allowPrivilegeEscalation == false`),
			jq.Match(`.spec.template.spec.containers[0].securityContext.runAsNonRoot == true`),
			jq.Match(`.spec.template.spec.containers[0].securityContext.capabilities.drop | any(. == "ALL")`),
			jq.Match(`.spec.template.spec.containers[0].securityContext.seccompProfile.type == "RuntimeDefault"`),
		)),
		WithCustomErrorMsg("dashboard-redirect Deployment should exist with correct nginx S2I configuration"),
	)

	// Wait for deployment readiness
	tc.EnsureDeploymentReady(types.NamespacedName{Name: gateway.DashboardRedirectName, Namespace: appNamespace}, 2)

	t.Log("Dashboard redirect Deployment validation completed")
}

// ValidateDashboardRedirectService validates the redirect service configuration.
func (tc *GatewayTestCtx) ValidateDashboardRedirectService(t *testing.T) {
	t.Helper()
	skipUnless(t, []TestTag{Tier1})
	t.Log("Validating dashboard redirect Service")

	appNamespace := tc.AppsNamespace

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Service, types.NamespacedName{
			Name:      gateway.DashboardRedirectName,
			Namespace: appNamespace,
		}),
		WithCondition(And(
			// Service type
			jq.Match(`.spec.type == "ClusterIP"`),

			// Labels
			jq.Match(`.metadata.labels.app == "%s"`, gateway.DashboardRedirectName),
			jq.Match(`.metadata.labels["%s"] == "%s"`, labels.PlatformPartOf, gateway.PartOfGatewayConfig),

			// Owner reference
			jq.Match(`.metadata.ownerReferences[] | select(.kind == "GatewayConfig") | .controller == true`),

			// Selector
			jq.Match(`.spec.selector.app == "%s"`, gateway.DashboardRedirectName),

			// Ports
			jq.Match(`.spec.ports | length == 1`),
			jq.Match(`.spec.ports[0].name == "http"`),
			jq.Match(`.spec.ports[0].port == 8080`),
			jq.Match(`.spec.ports[0].targetPort == 8080`),
			jq.Match(`.spec.ports[0].protocol == "TCP"`),
		)),
		WithCustomErrorMsg("dashboard-redirect Service should exist with correct configuration"),
	)

	t.Log("Dashboard redirect Service validation completed")
}

// ValidateDashboardRedirectRoutes validates both dashboard and legacy gateway redirect routes.
//
// This test verifies:
// - Dashboard route exists with platform-specific name (odh-dashboard or rhods-dashboard)
// - Legacy gateway route exists conditionally (only when current subdomain != data-science-gateway)
// - Routes point to dashboard-redirect service
// - Routes have proper TLS configuration
// - Routes have GatewayConfig owner references (verifies SSA ownership takeover).
func (tc *GatewayTestCtx) ValidateDashboardRedirectRoutes(t *testing.T) {
	t.Helper()
	skipUnless(t, []TestTag{Tier1})
	t.Log("Validating dashboard redirect Routes")

	appNamespace := tc.AppsNamespace
	dashboardRouteName := getDashboardRouteNameByPlatform(tc.FetchPlatformRelease())

	// Validate platform-specific dashboard route
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Route, types.NamespacedName{
			Name:      dashboardRouteName,
			Namespace: appNamespace,
		}),
		WithCondition(And(
			// Route target
			jq.Match(`.spec.to.kind == "Service"`),
			jq.Match(`.spec.to.name == "%s"`, gateway.DashboardRedirectName),
			jq.Match(`.spec.to.weight == 100`),

			// Port
			jq.Match(`.spec.port.targetPort == "http"`),

			// TLS configuration
			jq.Match(`.spec.tls.termination == "edge"`),
			jq.Match(`.spec.tls.insecureEdgeTerminationPolicy == "Redirect"`),

			// Owner reference - verifies SSA ownership takeover from old dashboard deployment
			jq.Match(`.metadata.ownerReferences | length > 0`),
			jq.Match(`.metadata.ownerReferences[] | select(.kind == "GatewayConfig") | .controller == true`),
			jq.Match(`.metadata.ownerReferences[] | select(.kind == "GatewayConfig") | .blockOwnerDeletion == true`),

			// Labels
			jq.Match(`.metadata.labels.app == "%s"`, gateway.DashboardRedirectName),
			jq.Match(`.metadata.labels["%s"] == "%s"`, labels.PlatformPartOf, gateway.PartOfGatewayConfig),

			// HSTS header annotation
			jq.Match(`.metadata.annotations."haproxy.router.openshift.io/hsts_header" != null`),
		)),
		WithCustomErrorMsg("Dashboard route %s should exist and point to dashboard-redirect service with GatewayConfig ownership", dashboardRouteName),
	)

	// Validate legacy gateway route (conditional based on current subdomain)
	// Only created when current subdomain != "data-science-gateway"
	currentSubdomain := gatewaySubdomain
	if currentSubdomain != gateway.LegacyGatewaySubdomain {
		t.Logf("Current gateway subdomain is %s, expecting legacy gateway redirect route to exist", currentSubdomain)

		tc.EnsureResourceExists(
			WithMinimalObject(gvk.Route, types.NamespacedName{
				Name:      gateway.LegacyGatewaySubdomain,
				Namespace: appNamespace,
			}),
			WithCondition(And(
				// Route target
				jq.Match(`.spec.to.kind == "Service"`),
				jq.Match(`.spec.to.name == "%s"`, gateway.DashboardRedirectName),

				// Port
				jq.Match(`.spec.port.targetPort == "http"`),

				// TLS configuration
				jq.Match(`.spec.tls.termination == "edge"`),
				jq.Match(`.spec.tls.insecureEdgeTerminationPolicy == "Redirect"`),

				// Owner reference
				jq.Match(`.metadata.ownerReferences[] | select(.kind == "GatewayConfig") | .controller == true`),

				// Labels
				jq.Match(`.metadata.labels["%s"] == "%s"`, labels.PlatformPartOf, gateway.PartOfGatewayConfig),
			)),
			WithCustomErrorMsg("Legacy gateway route %s should exist when current subdomain is %s", gateway.LegacyGatewaySubdomain, currentSubdomain),
		)
	} else {
		t.Logf("Current gateway subdomain is %s, legacy gateway redirect route should NOT be created", currentSubdomain)
	}

	t.Log("Dashboard redirect Routes validation completed")
}

// ValidateDashboardRedirectHTTP validates the HTTP redirect functionality.
//
// This test verifies end-to-end redirect behavior:
// - HTTP request to old dashboard route returns 301
// - Location header points to new gateway URL
// - Path is preserved in redirect ($request_uri works correctly).
func (tc *GatewayTestCtx) ValidateDashboardRedirectHTTP(t *testing.T) {
	t.Helper()
	skipUnless(t, []TestTag{Tier1})
	t.Log("Validating dashboard redirect HTTP functionality")

	dashboardRouteName := getDashboardRouteNameByPlatform(tc.FetchPlatformRelease())
	appNamespace := tc.AppsNamespace

	// Fetch the dashboard route to get its host
	routeObj := tc.EnsureResourceExists(
		WithMinimalObject(gvk.Route, types.NamespacedName{
			Name:      dashboardRouteName,
			Namespace: appNamespace,
		}),
		WithCustomErrorMsg("Dashboard redirect route should exist"),
	)

	// Extract the route host using jq
	dashboardRouteHost := ExtractAndExpectValue[string](tc.g, routeObj, ".spec.host", Not(BeEmpty()))

	// Get expected gateway hostname
	expectedGatewayHostname := tc.getExpectedGatewayHostname(t)

	// Test redirect with path preservation
	testPath := "/some/test/path"
	dashboardURL := "https://" + dashboardRouteHost + testPath
	expectedRedirectURL := "https://" + expectedGatewayHostname + testPath

	t.Logf("Testing redirect from %s to %s", dashboardURL, expectedRedirectURL)

	httpClient := tc.createHTTPClient()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dashboardURL, nil)
	tc.g.Expect(err).NotTo(HaveOccurred(), "Failed to create HTTP request")

	resp, err := httpClient.Do(req)
	tc.g.Expect(err).NotTo(HaveOccurred(), "Failed to make HTTP request to dashboard redirect route")
	defer resp.Body.Close()

	// Verify 301 Moved Permanently
	tc.g.Expect(resp.StatusCode).To(Equal(http.StatusMovedPermanently),
		"Expected 301 redirect, got %d", resp.StatusCode)

	// Verify Location header
	location := resp.Header.Get("Location")
	tc.g.Expect(location).To(Equal(expectedRedirectURL),
		"Redirect location should preserve path and point to new gateway URL")

	t.Logf("Redirect works correctly: %s -> %s", dashboardURL, location)
	t.Log("Dashboard redirect HTTP functionality validation completed")
}
