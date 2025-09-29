package gateway

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

const (
	// GatewayNamespace is the namespace where Gateway resources are deployed.
	GatewayNamespace = "openshift-ingress"

	// GatewayClassName is the name of the GatewayClass used for data science gateways.
	GatewayClassName = "data-science-gateway-class"

	// GatewayControllerName is the OpenShift Gateway API controller name.
	GatewayControllerName = "openshift.io/gateway-controller/v1"

	// Auth-related constants.
	AuthClientID = "odh"

	// Auth proxy constants.
	KubeAuthProxyName        = "kube-auth-proxy"
	KubeAuthProxySecretsName = "kube-auth-proxy-creds" //nolint:gosec // This is a resource name, not actual credentials
	KubeAuthProxyTLSName     = "kube-auth-proxy-tls"
	OAuthCallbackRouteName   = "oauth-callback-route"

	// Auth proxy configuration.
	AuthProxyHTTPPort   = 4180
	AuthProxyHTTPSPort  = 8443
	AuthProxyOAuth2Path = "/oauth2"
	TLSCertsVolumeName  = "tls-certs"
	TLSCertsMountPath   = "/etc/tls/private"

	// Secret generation lengths.
	ClientSecretLength     = 24
	CookieSecretLength     = 32
	DefaultClientSecretKey = "clientSecret"

	// Default gateway name used across all platforms.
	DefaultGatewayName = "data-science-gateway"

	// TODO: Replace with the correct image.
	KubeAuthProxyImage = "quay.io/jtanner/kube-auth-proxy@sha256:434580fd42d73727d62566ff6d8336219a31b322798b48096ed167daaec42f07"
)

var (
	// Common labels for auth proxy resources.
	KubeAuthProxyLabels = map[string]string{"app": KubeAuthProxyName}
)

// GetCertificateType returns a string representation of the certificate type.
func GetCertificateType(gatewayConfig *serviceApi.GatewayConfig) string {
	if gatewayConfig.Spec.Certificate == nil {
		return string(infrav1.OpenshiftDefaultIngress)
	}
	return string(gatewayConfig.Spec.Certificate.Type)
}

func ResolveDomain(ctx context.Context, client client.Client,
	gatewayConfig *serviceApi.GatewayConfig, gatewayName string) (string, error) {
	// Determine base domain
	baseDomain := strings.TrimSpace(gatewayConfig.Spec.Domain)
	if baseDomain == "" {
		var err error
		baseDomain, err = cluster.GetDomain(ctx, client)
		if err != nil {
			return "", fmt.Errorf("failed to get cluster domain: %w", err)
		}
	}

	// Combine gateway name with base domain
	// e.g. data-science-gateway.apps.example.com
	return fmt.Sprintf("%s.%s", gatewayName, baseDomain), nil
}

// CreateListeners creates the Gateway listeners configuration.
func CreateListeners(certSecretName string, domain string) []gwapiv1.Listener {
	listeners := []gwapiv1.Listener{}

	if certSecretName != "" {
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
	}

	return listeners
}

// CreateErrorCondition creates a standardized error condition for gateway operations.
func CreateErrorCondition(message string, err error) common.Condition {
	fullMessage := message
	if err != nil {
		fullMessage = fmt.Sprintf("%s: %v", message, err)
	}

	return common.Condition{
		Type:    status.ConditionTypeReady,
		Status:  metav1.ConditionFalse,
		Reason:  status.NotReadyReason,
		Message: fullMessage,
	}
}
