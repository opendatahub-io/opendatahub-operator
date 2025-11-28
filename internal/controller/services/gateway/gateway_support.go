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
	// Gateway infrastructure constants.
	GatewayNamespace      = "openshift-ingress"                  // Namespace where Gateway resources are deployed
	GatewayClassName      = "data-science-gateway-class"         // GatewayClass name for data science gateways
	GatewayControllerName = "openshift.io/gateway-controller/v1" // OpenShift Gateway API controller name
	DefaultGatewayName    = "data-science-gateway"               // Default gateway name used across all platforms

	// Authentication constants.
	AuthClientID             = "data-science" // OauthClient name.
	KubeAuthProxyName        = "kube-auth-proxy"
	KubeAuthProxySecretsName = "kube-auth-proxy-creds" //nolint:gosec // This is a resource name, not actual credentials
	KubeAuthProxyTLSName     = "kube-auth-proxy-tls"   //nolint:gosec
	OAuthCallbackRouteName   = "oauth-callback-route"
	AuthnFilterName          = "data-science-authn-filter"
	DestinationRuleName      = "data-science-tls-rule"

	// Network configuration.
	AuthProxyHTTPPort    = 4180
	AuthProxyMetricsPort = 9000
	StandardHTTPSPort    = 443
	GatewayHTTPSPort     = 8443

	AuthProxyOAuth2Path = "/oauth2"
	// OAuth2 proxy cookie name - used in both proxy args and EnvoyFilter Lua filter.
	AuthProxyCookieName = "_oauth2_proxy"

	// Volume and mount paths.
	TLSCertsVolumeName = "tls-certs"
	TLSCertsMountPath  = "/etc/tls/private"

	// Secret configuration.
	DefaultGatewayTLSSecretName = "data-science-gatewayconfig-tls"

	// Environment variable names for OAuth2 proxy.
	EnvClientID     = "OAUTH2_PROXY_CLIENT_ID"
	EnvClientSecret = "OAUTH2_PROXY_CLIENT_SECRET" //nolint:gosec // This is an environment variable name, not a secret
	EnvCookieSecret = "OAUTH2_PROXY_COOKIE_SECRET" //nolint:gosec // This is an environment variable name, not a secret

	// Label constants.
	ComponentLabelValue = "authentication"
	PartOfLabelValue    = "data-science-gateway"
	PartOfGatewayConfig = "gatewayconfig"
)

const (
	envoyFilterTemplate                  = "resources/envoyfilter-authn.tmpl.yaml"
	destinationRuleTemplate              = "resources/kube-auth-proxy-destinationrule-tls.tmpl.yaml"
	kubeAuthProxyDeploymentOidcTemplate  = "resources/kube-auth-proxy-oidc-deployment.tmpl.yaml"
	kubeAuthProxyDeploymentOauthTemplate = "resources/kube-auth-proxy-oauth-deployment.tmpl.yaml"
	kubeAuthProxyServiceTemplate         = "resources/kube-auth-proxy-svc.tmpl.yaml"
	kubeAuthProxyHTTPRouteTemplate       = "resources/kube-auth-proxy-httproute.tmpl.yaml"
	networkPolicyTemplate                = "resources/kube-auth-proxy-networkpolicy.yaml"
)

// GetFQDN returns the fully qualified domain name for the gateway based on the GatewayConfig.
// It constructs the FQDN by combining the subdomain (or default) with either the user-specified
// domain or the cluster domain.
func GetFQDN(ctx context.Context, cli client.Client, gatewayConfig *serviceApi.GatewayConfig) (string, error) {
	subdomain := DefaultGatewayName

	if gatewayConfig != nil {
		subdomain = strings.TrimSpace(gatewayConfig.Spec.Subdomain)
		if subdomain == "" {
			subdomain = DefaultGatewayName
		}

		baseDomain := strings.TrimSpace(gatewayConfig.Spec.Domain)
		if baseDomain != "" {
			return fmt.Sprintf("%s.%s", subdomain, baseDomain), nil
		}
	}

	clusterDomain, err := cluster.GetDomain(ctx, cli)
	if err != nil {
		return "", fmt.Errorf("failed to get cluster domain: %w", err)
	}

	return fmt.Sprintf("%s.%s", subdomain, clusterDomain), nil
}

// GetGatewayDomain reads the domain directly from the Gateway CR's listener hostname.
// Falls back to GatewayConfig if Gateway CR doesn't exist yet.
func GetGatewayDomain(ctx context.Context, cli client.Client) (string, error) {
	// Try to get the Gateway CR first
	gateway := &gwapiv1.Gateway{}
	err := cli.Get(ctx, client.ObjectKey{
		Name:      DefaultGatewayName,
		Namespace: GatewayNamespace,
	}, gateway)
	if err == nil {
		if len(gateway.Spec.Listeners) > 0 && gateway.Spec.Listeners[0].Hostname != nil {
			return string(*gateway.Spec.Listeners[0].Hostname), nil
		}
	}

	gatewayConfig := &serviceApi.GatewayConfig{}
	err = cli.Get(ctx, client.ObjectKey{Name: serviceApi.GatewayConfigName}, gatewayConfig)
	if err != nil {
		// GatewayConfig doesn't exist either, use cluster domain with default subdomain
		return GetFQDN(ctx, cli, nil)
	}

	return GetFQDN(ctx, cli, gatewayConfig)
}

// This helper function optimizes the condition checking logic.
func isGatewayReady(gateway *gwapiv1.Gateway) bool {
	if gateway == nil {
		return false
	}
	for _, condition := range gateway.Status.Conditions {
		if condition.Type == string(gwapiv1.GatewayConditionAccepted) && condition.Status == metav1.ConditionTrue {
			return true
		}
	}
	return false
}

// getCertificateType returns a string representation of the certificate type.
func getCertificateType(gatewayConfig *serviceApi.GatewayConfig) string {
	if gatewayConfig == nil {
		return string(infrav1.OpenshiftDefaultIngress)
	}
	if gatewayConfig.Spec.Certificate == nil || gatewayConfig.Spec.Certificate.Type == "" {
		return string(infrav1.OpenshiftDefaultIngress)
	}
	return string(gatewayConfig.Spec.Certificate.Type)
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

// createOAuthClient creates an OpenShift OAuth client for integrated authentication.
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
		labels.PlatformPartOf: PartOfGatewayConfig,
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

// validateGatewayConfig extracts the GatewayConfig from the reconciliation request instance.
// Returns an error if the instance is not of type *serviceApi.GatewayConfig.
func validateGatewayConfig(rr *odhtypes.ReconciliationRequest) (*serviceApi.GatewayConfig, error) {
	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return nil, errors.New("instance is not of type *services.GatewayConfig")
	}
	return gatewayConfig, nil
}

// getGatewayAuthProxyTimeout returns the auth timeout using:
// Deprecated AuthTimeout field > AuthProxyTimeout field > default (5s).
func getGatewayAuthProxyTimeout(gatewayConfig *serviceApi.GatewayConfig) string {
	if gatewayConfig != nil {
		// Check deprecated field first for backward compatibility
		if gatewayConfig.Spec.AuthTimeout != "" {
			return gatewayConfig.Spec.AuthTimeout
		}
		// Check new field
		if gatewayConfig.Spec.AuthProxyTimeout.Duration != 0 {
			return gatewayConfig.Spec.AuthProxyTimeout.Duration.String()
		}
	}

	return "5s"
}

// getCookieSettings returns cookie expire and refresh durations with defaults.
func getCookieSettings(cookieConfig *serviceApi.CookieConfig) (string, string) {
	// Set defaults
	expire, refresh := "24h0m0s", "1h0m0s"

	// Override with user configuration if provided
	if cookieConfig != nil {
		if cookieConfig.Expire.Duration != 0 {
			expire = cookieConfig.Expire.Duration.String()
		}
		if cookieConfig.Refresh.Duration != 0 {
			refresh = cookieConfig.Refresh.Duration.String()
		}
	}

	return expire, refresh
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

// getKubeAuthProxyImage returns the kube-auth-proxy image from environment variable.
// For RHOAI deployments, this comes from the CSV (via RHOAI-Build-Config/bundle/additional-images-patch.yaml).
// For ODH deployments, this comes from config/manager/manager.yaml.
// Falls back to a default image for local development/testing only.
func getKubeAuthProxyImage() string {
	if image := os.Getenv("RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE"); image != "" {
		return image
	}
	// Fallback for ODH development
	return "quay.io/opendatahub/odh-kube-auth-proxy:latest"
}

func getAuthProxySecretValues(
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
