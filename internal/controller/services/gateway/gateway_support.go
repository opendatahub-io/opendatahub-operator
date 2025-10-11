package gateway

import (
	"context"
	"fmt"
	"os"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

const (
	// Gateway infrastructure constants.
	GatewayNamespace      = "openshift-ingress"                  // Namespace where Gateway resources are deployed
	GatewayClassName      = "data-science-gateway-class"         // GatewayClass name for data science gateways
	GatewayControllerName = "openshift.io/gateway-controller/v1" // OpenShift Gateway API controller name
	DefaultGatewayName    = "data-science-gateway"               // Default gateway name used across all platforms

	// Authentication constants.
	AuthClientID        = "odh"       // OAuth client ID
	OpenShiftOAuthScope = "user:full" // OAuth client scope

	// OAuth2 proxy infrastructure.
	KubeAuthProxyName        = "kube-auth-proxy"
	KubeAuthProxySecretsName = "kube-auth-proxy-creds" //nolint:gosec // This is a resource name, not actual credentials
	KubeAuthProxyTLSName     = "kube-auth-proxy-tls"
	OAuthCallbackRouteName   = "oauth-callback-route"

	// Network configuration.
	AuthProxyHTTPPort   = 4180
	AuthProxyHTTPSPort  = 8443
	AuthProxyOAuth2Path = "/oauth2"

	// Volume and mount paths.
	TLSCertsVolumeName = "tls-certs"
	TLSCertsMountPath  = "/etc/tls/private"

	// Secret configuration.
	ClientSecretLength     = 24
	CookieSecretLength     = 32
	DefaultClientSecretKey = "clientSecret"

	// Environment variable names for OAuth2 proxy.
	EnvClientID     = "OAUTH2_PROXY_CLIENT_ID"
	EnvClientSecret = "OAUTH2_PROXY_CLIENT_SECRET" //nolint:gosec // This is an environment variable name, not a secret
	EnvCookieSecret = "OAUTH2_PROXY_COOKIE_SECRET" //nolint:gosec // This is an environment variable name, not a secret
)

var (
	// KubeAuthProxyLabels provides common labels for OAuth2 proxy resources.
	KubeAuthProxyLabels = map[string]string{"app": KubeAuthProxyName}
)

// getKubeAuthProxyImage returns the kube-auth-proxy image from environment variable.
// For RHOAI deployments, this comes from the CSV (via RHOAI-Build-Config/bundle/additional-images-patch.yaml).
// For ODH deployments, this comes from config/manager/manager.yaml.
// Falls back to a default image for local development/testing only.
func getKubeAuthProxyImage() string {
	if image := os.Getenv("RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE"); image != "" {
		return image
	}
	// Fallback for local development only
	return "quay.io/jtanner/kube-auth-proxy:latest"
}

// GetCertificateType returns a string representation of the certificate type.
func GetCertificateType(gatewayConfig *serviceApi.GatewayConfig) string {
	if gatewayConfig == nil {
		return string(infrav1.OpenshiftDefaultIngress)
	}
	if gatewayConfig.Spec.Certificate == nil {
		return string(infrav1.OpenshiftDefaultIngress)
	}
	return string(gatewayConfig.Spec.Certificate.Type)
}

// buildGatewayDomain combines gateway name with base domain.
func buildGatewayDomain(baseDomain string) string {
	// Use string concatenation for better performance in frequently called function
	return DefaultGatewayName + "." + baseDomain
}

// getClusterDomain gets cluster domain - extracted common logic.
func getClusterDomain(ctx context.Context, client client.Client) (string, error) {
	clusterDomain, err := cluster.GetDomain(ctx, client)
	if err != nil {
		return "", fmt.Errorf("failed to get cluster domain: %w", err)
	}
	return buildGatewayDomain(clusterDomain), nil
}

func ResolveDomain(ctx context.Context, client client.Client,
	gatewayConfig *serviceApi.GatewayConfig) (string, error) {
	// Input validation
	if gatewayConfig == nil {
		return getClusterDomain(ctx, client)
	}

	// Check if user has overridden the domain
	baseDomain := strings.TrimSpace(gatewayConfig.Spec.Domain)
	if baseDomain != "" {
		return buildGatewayDomain(baseDomain), nil
	}

	// No domain override, use cluster domain
	return getClusterDomain(ctx, client)
}

// GetGatewayDomain reads GatewayConfig and passes it to ResolveDomain.
// This function optimizes API calls by handling the GatewayConfig retrieval
// and domain resolution in a single flow.
func GetGatewayDomain(ctx context.Context, cli client.Client) (string, error) {
	// Try to get the GatewayConfig
	gatewayConfig := &serviceApi.GatewayConfig{}
	err := cli.Get(ctx, client.ObjectKey{Name: serviceApi.GatewayInstanceName}, gatewayConfig)
	if err != nil {
		// GatewayConfig doesn't exist, use cluster domain directly
		return getClusterDomain(ctx, cli)
	}

	// Check if user has overridden the domain (inline ResolveDomain logic to avoid redundant calls)
	baseDomain := strings.TrimSpace(gatewayConfig.Spec.Domain)
	if baseDomain != "" {
		return buildGatewayDomain(baseDomain), nil
	}

	// No domain override, use cluster domain
	return getClusterDomain(ctx, cli)
}

// CreateListeners creates the Gateway listeners configuration.
func CreateListeners(certSecretName string, domain string) []gwapiv1.Listener {
	// Early return for empty certificate - avoid unnecessary allocations
	if certSecretName == "" {
		return nil
	}

	// Pre-allocate slice with known capacity to avoid reallocations
	listeners := make([]gwapiv1.Listener, 0, 1)

	from := gwapiv1.NamespacesFromAll
	httpsMode := gwapiv1.TLSModeTerminate
	hostname := gwapiv1.Hostname(domain)

	httpsListener := gwapiv1.Listener{
		Name:     "https",
		Protocol: gwapiv1.HTTPSProtocolType,
		Port:     443,
		Hostname: &hostname,
		TLS: &gwapiv1.GatewayTLSConfig{
			Mode: &httpsMode,
			CertificateRefs: []gwapiv1.SecretObjectReference{
				{
					Name: gwapiv1.ObjectName(certSecretName),
				},
			},
		},
		AllowedRoutes: &gwapiv1.AllowedRoutes{
			Namespaces: &gwapiv1.RouteNamespaces{
				From: &from,
			},
		},
	}

	listeners = append(listeners, httpsListener)
	return listeners
}
