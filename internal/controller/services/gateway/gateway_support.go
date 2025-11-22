package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	oauthv1 "github.com/openshift/api/oauth/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	ServiceName        = serviceApi.GatewayServiceName
	ReadyConditionType = serviceApi.GatewayConfigKind + status.ReadySuffix
)

const (
	// for gateway part.
	GatewayNamespace            = "openshift-ingress" // GatewayNamespace is the namespace where Gateway resources are deployed.
	DefaultGatewayName          = serviceApi.DefaultGatewayName
	GatewayClassName            = "data-science-gateway-class"         // GatewayClassName is the name of the GatewayClass used for data science gateways.
	GatewayControllerName       = "openshift.io/gateway-controller/v1" // OpenShift Gateway API controller name
	DefaultGatewayTLSSecretName = "data-science-gatewayconfig-tls"
	StandardHTTPSPort           = 443
	GatewayHTTPSPort            = 8443
	// for auth part.
	AuthClientID             = "data-science" // OauthClient name.
	KubeAuthProxyName        = "kube-auth-proxy"
	KubeAuthProxySecretsName = "kube-auth-proxy-creds" //nolint:gosec // This is a resource name, not actual credentials
	KubeAuthProxyTLSName     = "kube-auth-proxy-tls"   //nolint:gosec
	OAuthCallbackRouteName   = "oauth-callback-route"
	AuthnFilterName          = "data-science-authn-filter"
	DestinationRuleName      = "data-science-tls-rule"

	AuthProxyHTTPPort    = 4180
	AuthProxyMetricsPort = 9000

	AuthProxyOAuth2Path = "/oauth2"
	AuthProxyCookieName = "_oauth2_proxy"
	TLSCertsVolumeName  = "tls-certs"
	TLSCertsMountPath   = "/etc/tls/private"

	// Secret generation lengths.
	ClientSecretLength     = 24
	CookieSecretLength     = 32
	DefaultClientSecretKey = "clientSecret"

	// Environment variable names for OAuth2 proxy.
	EnvClientID     = "OAUTH2_PROXY_CLIENT_ID"
	EnvClientSecret = "OAUTH2_PROXY_CLIENT_SECRET" //nolint:gosec // This is an environment variable name, not a secret
	EnvCookieSecret = "OAUTH2_PROXY_COOKIE_SECRET" //nolint:gosec // This is an environment variable name, not a secret

	// Default KubeAuthProxy image - can be overridden by environment variable.
	// TODO: this should be from quay.io/modh.
	DefaultKubeAuthProxyImage = "quay.io/opendatahub/odh-kube-auth-proxy:latest"
)

// GetKubeAuthProxyImage returns the KubeAuthProxy image to use.
// It first checks the RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE environment variable, and falls back to the default image if not set.
func GetKubeAuthProxyImage() string {
	if image := os.Getenv("RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE"); image != "" {
		return image
	}
	return DefaultKubeAuthProxyImage
}

// GetFQDN returns the FQDN for the gateway. It first checks if a domain is specified in the GatewayConfig,
// and falls back to the cluster domain. The subdomain can be customized or defaults to DefaultGatewayName.
func GetFQDN(ctx context.Context, cli client.Client, gatewayConfig *serviceApi.GatewayConfig) (string, error) {
	subdomain := strings.TrimSpace(gatewayConfig.Spec.Subdomain)
	if subdomain == "" {
		subdomain = DefaultGatewayName
	}

	baseDomain := strings.TrimSpace(gatewayConfig.Spec.Domain)
	if baseDomain != "" {
		return fmt.Sprintf("%s.%s", subdomain, baseDomain), nil
	}

	clusterDomain, err := cluster.GetDomain(ctx, cli)
	if err != nil {
		return "", fmt.Errorf("failed to get cluster domain: %w", err)
	}

	return fmt.Sprintf("%s.%s", subdomain, clusterDomain), nil
}

func createGatewayClass(rr *odhtypes.ReconciliationRequest) error {
	gatewayClass := &gwapiv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: GatewayClassName,
		},
		Spec: gwapiv1.GatewayClassSpec{
			ControllerName: GatewayControllerName,
		},
	}

	return rr.AddResources(gatewayClass)
}

func getCertificateType(gatewayConfig *serviceApi.GatewayConfig) string {
	if gatewayConfig.Spec.Certificate == nil {
		return string(infrav1.OpenshiftDefaultIngress)
	}
	certType := gatewayConfig.Spec.Certificate.Type
	if certType == "" { // zero value
		return string(infrav1.OpenshiftDefaultIngress)
	}
	return string(certType)
}

func createGateway(rr *odhtypes.ReconciliationRequest, certSecretName string, domain string) error {
	listeners := []gwapiv1.Listener{}

	if certSecretName != "" {
		allowedNamespaces := gwapiv1.NamespacesFromSelector
		httpsMode := gwapiv1.TLSModeTerminate
		hostname := gwapiv1.Hostname(domain)
		httpsListener := gwapiv1.Listener{
			Name:     "https",
			Protocol: gwapiv1.HTTPSProtocolType,
			Port:     StandardHTTPSPort,
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
					From: &allowedNamespaces,
					Selector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "kubernetes.io/metadata.name",
								Operator: metav1.LabelSelectorOpIn,
								Values: []string{
									GatewayNamespace,
									cluster.GetApplicationNamespace(),
								},
							},
						},
					},
				},
			},
		}
		listeners = append(listeners, httpsListener)
	}

	gateway := &gwapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DefaultGatewayName,
			Namespace: GatewayNamespace,
		},
		Spec: gwapiv1.GatewaySpec{
			GatewayClassName: GatewayClassName,
			Listeners:        listeners,
		},
	}

	return rr.AddResources(gateway)
}

func handleCertificates(ctx context.Context, rr *odhtypes.ReconciliationRequest, gatewayConfig *serviceApi.GatewayConfig, domain string) (string, error) {
	var certConfig infrav1.CertificateSpec
	if gatewayConfig.Spec.Certificate != nil {
		certConfig = *gatewayConfig.Spec.Certificate
	}

	if certConfig.Type == "" {
		certConfig.Type = infrav1.OpenshiftDefaultIngress
	}

	secretName := certConfig.SecretName
	if secretName == "" {
		secretName = fmt.Sprintf("%s-tls", gatewayConfig.Name)
	}

	switch certConfig.Type {
	case infrav1.OpenshiftDefaultIngress:
		if err := cluster.PropagateDefaultIngressCertificate(ctx, rr.Client, secretName, GatewayNamespace,
			cluster.WithLabels( // add label easy to know it is from us.
				labels.PlatformPartOf, ServiceName,
			),
			cluster.OwnedBy(gatewayConfig, rr.Client.Scheme()), // set ownerreference for cleanup
		); err != nil {
			return "", fmt.Errorf("failed to propagate default ingress certificate: %w", err)
		}
		return secretName, nil
	case infrav1.SelfSigned:
		hostname := fmt.Sprintf("%s.%s", DefaultGatewayName, domain)
		if err := cluster.CreateSelfSignedCertificate(ctx, rr.Client, secretName, hostname, GatewayNamespace,
			cluster.WithLabels( // add label easy to know it is from us.
				labels.PlatformPartOf, ServiceName,
			),
			cluster.OwnedBy(gatewayConfig, rr.Client.Scheme()), // set ownerreference for cleanup
		); err != nil {
			return "", fmt.Errorf("failed to create self-signed certificate: %w", err)
		}
		return secretName, nil
	case infrav1.Provided:
		return secretName, nil
	default:
		return "", fmt.Errorf("unsupported certificate type: %s", certConfig.Type)
	}
}

// createSecret dynamically creates the kube-auth-proxy-creds secret immediately on the cluster.
func createSecret(ctx context.Context, rr *odhtypes.ReconciliationRequest, clientID, clientSecret, cookieSecret string) error {
	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return errors.New("instance is not of type *services.GatewayConfig")
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KubeAuthProxySecretsName,
			Namespace: GatewayNamespace,
		},
		Type: corev1.SecretTypeOpaque,
	}

	labelList := map[string]string{
		labels.PlatformPartOf: "gatewayconfig",
	}

	_, err := controllerutil.CreateOrUpdate(ctx, rr.Client, secret, func() error {
		secret.StringData = map[string]string{
			"OAUTH2_PROXY_CLIENT_ID":     clientID,
			"OAUTH2_PROXY_CLIENT_SECRET": clientSecret,
			"OAUTH2_PROXY_COOKIE_SECRET": cookieSecret,
		}
		resources.SetLabels(secret, labelList)
		return controllerutil.SetControllerReference(gatewayConfig, secret, rr.Client.Scheme())
	})

	return err
}

func createOAuthClient(ctx context.Context, rr *odhtypes.ReconciliationRequest, gatewayConfig *serviceApi.GatewayConfig) error {
	// Read client secret from kube-auth-proxy-creds secret
	authSecret := &corev1.Secret{}
	if err := rr.Client.Get(ctx, types.NamespacedName{
		Name:      KubeAuthProxySecretsName,
		Namespace: GatewayNamespace,
	}, authSecret); err != nil {
		return fmt.Errorf("failed to get auth proxy secret %s/%s: %w", GatewayNamespace, KubeAuthProxySecretsName, err)
	}

	clientSecretBytes, exists := authSecret.Data["OAUTH2_PROXY_CLIENT_SECRET"]
	if !exists {
		return fmt.Errorf("OAUTH2_PROXY_CLIENT_SECRET not found in secret %s/%s", GatewayNamespace, KubeAuthProxySecretsName)
	}
	clientSecret := string(clientSecretBytes)

	domain, err := GetFQDN(ctx, rr.Client, gatewayConfig)
	if err != nil {
		return fmt.Errorf("failed to resolve domain: %w", err)
	}
	redirectURL := fmt.Sprintf("https://%s/oauth2/callback", domain)
	oauthClient := &oauthv1.OAuthClient{
		ObjectMeta: metav1.ObjectMeta{
			Name: AuthClientID,
		},
		GrantMethod:  oauthv1.GrantHandlerAuto,
		RedirectURIs: []string{redirectURL},
		Secret:       clientSecret, // encoded string
	}

	return rr.AddResources(oauthClient)
}

func getSecretValues(
	ctx context.Context,
	rr *odhtypes.ReconciliationRequest,
	authMode cluster.AuthenticationMode,
	oidcConfig *serviceApi.OIDCConfig) (string, string, string, error) {
	// Check if kube-auth-proxy-creds already exists and is valid
	existingSecret := &corev1.Secret{}
	secretErr := rr.Client.Get(ctx, types.NamespacedName{
		Name:      KubeAuthProxySecretsName,
		Namespace: GatewayNamespace,
	}, existingSecret)

	// Fast exit on NotFound errors
	if secretErr != nil && !k8serr.IsNotFound(secretErr) {
		return "", "", "", fmt.Errorf("failed to check existing secret %s/%s: %w", GatewayNamespace, KubeAuthProxySecretsName, secretErr)
	}

	// If secret exists, validate and reuse its values
	if secretErr == nil {
		clientSecretBytes, hasClientSecret := existingSecret.Data["OAUTH2_PROXY_CLIENT_SECRET"]
		cookieSecretBytes, hasCookieSecret := existingSecret.Data["OAUTH2_PROXY_COOKIE_SECRET"]
		clientIDBytes, hasClientID := existingSecret.Data["OAUTH2_PROXY_CLIENT_ID"]

		if hasClientSecret && hasCookieSecret && hasClientID {
			return string(clientIDBytes), string(clientSecretBytes), string(cookieSecretBytes), nil
		}
	}

	var clientSecretValue, clientID string

	switch authMode {
	case cluster.AuthModeOIDC:
		// OIDC mode: get client secret from external secret
		clientID = oidcConfig.ClientID

		// Determine which namespace to use for the secret
		secretNamespace := oidcConfig.SecretNamespace
		if secretNamespace == "" {
			secretNamespace = GatewayNamespace // Default to openshift-ingress if not specified
		}

		externalSecret := &corev1.Secret{}
		if err := rr.Client.Get(ctx, types.NamespacedName{
			Name:      oidcConfig.ClientSecretRef.Name,
			Namespace: secretNamespace,
		}, externalSecret); err != nil {
			return "", "", "", fmt.Errorf("failed to get OIDC client secret %s/%s: %w",
				secretNamespace, oidcConfig.ClientSecretRef.Name, err)
		}

		key := oidcConfig.ClientSecretRef.Key
		if key == "" {
			key = "clientSecret"
		}

		if secretValue, exists := externalSecret.Data[key]; exists {
			clientSecretValue = string(secretValue)
		} else {
			return "", "", "", fmt.Errorf("key '%s' not found in OIDC secret %s/%s", key, secretNamespace, oidcConfig.ClientSecretRef.Name)
		}

	case cluster.AuthModeIntegratedOAuth:
		// OAuth mode: generate new client secret
		clientID = AuthClientID

		clientSecretGen, err := cluster.NewSecret("client-secret", "random", 24)
		if err != nil {
			return "", "", "", fmt.Errorf("failed to generate client secret: %w", err)
		}
		clientSecretValue = clientSecretGen.Value

	default:
		return "", "", "", fmt.Errorf("auth mode: %s is not supported", authMode)
	}

	// Always generate new cookie secret on oauth or oidc mode.
	cookieSecretGen, err := cluster.NewSecret("cookie-secret", "random", 32)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to generate cookie secret: %w", err)
	}

	return clientID, clientSecretValue, cookieSecretGen.Value, nil
}

// calculateAuthConfigHash generates a hash of the authentication secret values
// to detect changes that should trigger a kube-auth-proxy pod restart.
func calculateAuthConfigHash(authSecret *corev1.Secret) string {
	clientID := string(authSecret.Data["OAUTH2_PROXY_CLIENT_ID"])
	clientSecret := string(authSecret.Data["OAUTH2_PROXY_CLIENT_SECRET"])
	cookieSecret := string(authSecret.Data["OAUTH2_PROXY_COOKIE_SECRET"])

	// Calculate SHA256 hash
	hash := sha256.Sum256([]byte(clientID + clientSecret + cookieSecret))
	return hex.EncodeToString(hash[:])
}
