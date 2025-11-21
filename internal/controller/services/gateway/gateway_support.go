package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	oauthv1 "github.com/openshift/api/oauth/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// AuthMode represents different authentication modes supported by the gateway.
type AuthMode string

const (
	// Gateway infrastructure constants.
	GatewayNamespace      = "openshift-ingress"                  // Namespace where Gateway resources are deployed
	GatewayClassName      = "data-science-gateway-class"         // GatewayClass name for data science gateways
	GatewayControllerName = "openshift.io/gateway-controller/v1" // OpenShift Gateway API controller name
	DefaultGatewayName    = "data-science-gateway"               // Default gateway name used across all platforms

	// Authentication constants.
	AuthClientID        = "data-science" // OAuth client ID
	OpenShiftOAuthScope = "user:full"    // OAuth client scope

	// OAuth2 proxy infrastructure.
	KubeAuthProxyName        = "kube-auth-proxy"
	KubeAuthProxySecretsName = "kube-auth-proxy-creds" //nolint:gosec // This is a resource name, not actual credentials
	KubeAuthProxyTLSName     = "kube-auth-proxy-tls"
	OAuthCallbackRouteName   = "oauth-callback-route"

	// Network configuration.
	AuthProxyHTTPPort    = 4180
	AuthProxyHTTPSPort   = 8443
	AuthProxyMetricsPort = 9000
	AuthProxyOAuth2Path  = "/oauth2"

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

	// OAuth2 proxy cookie name - used in both proxy args and EnvoyFilter Lua filter.
	OAuth2ProxyCookieName = "_oauth2_proxy"

	AuthModeIntegratedOAuth AuthMode = "IntegratedOAuth"
	AuthModeOIDC            AuthMode = "OIDC"
	AuthModeNone            AuthMode = "None"
)

var (
	// KubeAuthProxyLabels provides common labels for OAuth2 proxy resources.
	KubeAuthProxyLabels = map[string]string{"app": KubeAuthProxyName}
)

// make secret data into sha256 as hash.
func calculateSecretHash(secretData map[string][]byte) string {
	clientID := string(secretData[EnvClientID])
	clientSecret := string(secretData[EnvClientSecret])
	cookieSecret := string(secretData[EnvCookieSecret])

	configData := clientID + clientSecret + cookieSecret
	hash := sha256.Sum256([]byte(configData))
	return hex.EncodeToString(hash[:])
}

// getKubeAuthProxyImage returns the kube-auth-proxy image from environment variable.
// Falls back to a odh image for development.
func getKubeAuthProxyImage() string {
	if image := os.Getenv("RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE"); image != "" {
		return image
	}
	// Fallback for ODH development
	return "quay.io/opendatahub/odh-kube-auth-proxy:latest"
}

// getCertificateType returns a string representation of the certificate type.
func getCertificateType(gatewayConfig *serviceApi.GatewayConfig) string {
	if gatewayConfig == nil {
		return string(infrav1.OpenshiftDefaultIngress)
	}
	if gatewayConfig.Spec.Certificate == nil {
		return string(infrav1.OpenshiftDefaultIngress)
	}
	return string(gatewayConfig.Spec.Certificate.Type)
}

// buildGatewayDomain combines subdomain with base domain.
// If subdomain is empty or whitespace, uses DefaultGatewayName as fallback.
func buildGatewayDomain(subdomain, baseDomain string) string {
	// Trim whitespace and use provided subdomain or fallback to default gateway name
	hostname := strings.TrimSpace(subdomain)
	if hostname == "" {
		hostname = DefaultGatewayName
	}
	// Use string concatenation for better performance in frequently called function
	return hostname + "." + baseDomain
}

// getClusterDomain gets cluster domain - extracted common logic.
func getClusterDomain(ctx context.Context, client client.Client, subdomain string) (string, error) {
	clusterDomain, err := cluster.GetDomain(ctx, client)
	if err != nil {
		return "", fmt.Errorf("failed to get cluster domain: %w", err)
	}
	return buildGatewayDomain(subdomain, clusterDomain), nil
}

func resolveDomain(ctx context.Context, client client.Client,
	gatewayConfig *serviceApi.GatewayConfig) (string, error) {
	// Input validation
	if gatewayConfig == nil {
		return getClusterDomain(ctx, client, "")
	}

	// Extract subdomain from GatewayConfig if provided
	subdomain := strings.TrimSpace(gatewayConfig.Spec.Subdomain)

	// Check if user has overridden the domain
	baseDomain := strings.TrimSpace(gatewayConfig.Spec.Domain)
	if baseDomain != "" {
		return buildGatewayDomain(subdomain, baseDomain), nil
	}

	// No domain override, use cluster domain with subdomain
	return getClusterDomain(ctx, client, subdomain)
}

// GetGatewayDomain reads GatewayConfig and passes it to resolveDomain.
// This function optimizes API calls by handling the GatewayConfig retrieval
// and domain resolution in a single flow.
func GetGatewayDomain(ctx context.Context, cli client.Client) (string, error) {
	// Try to get the GatewayConfig
	gatewayConfig := &serviceApi.GatewayConfig{}
	err := cli.Get(ctx, client.ObjectKey{Name: serviceApi.GatewayInstanceName}, gatewayConfig)
	if err != nil {
		// GatewayConfig doesn't exist, use cluster domain directly with default subdomain
		return getClusterDomain(ctx, cli, "")
	}

	// Extract subdomain from GatewayConfig if provided
	subdomain := strings.TrimSpace(gatewayConfig.Spec.Subdomain)

	// Check if user has overridden the domain (inline ResolveDomain logic to avoid redundant calls)
	baseDomain := strings.TrimSpace(gatewayConfig.Spec.Domain)
	if baseDomain != "" {
		return buildGatewayDomain(subdomain, baseDomain), nil
	}

	// No domain override, use cluster domain with subdomain
	return getClusterDomain(ctx, cli, subdomain)
}

// createListeners creates the Gateway listeners configuration with namespace restrictions.
func createListeners(certSecretName string, domain string) []gwapiv1.Listener {
	// Early return for empty certificate - avoid unnecessary allocations
	if certSecretName == "" {
		return nil
	}

	// Pre-allocate slice with known capacity to avoid reallocations
	listeners := make([]gwapiv1.Listener, 0, 1)

	selectorMode := gwapiv1.NamespacesFromSelector
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
				From: &selectorMode,
				Selector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "kubernetes.io/metadata.name",
							Operator: metav1.LabelSelectorOpIn,
							Values: []string{
								GatewayNamespace,                  // openshift-ingress
								cluster.GetApplicationNamespace(), // opendatahub or redhat-ods-applications
							},
						},
					},
				},
			},
		},
	}

	listeners = append(listeners, httpsListener)
	return listeners
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

func createGatewayClass(rr *odhtypes.ReconciliationRequest) error {
	gatewayClass := &gwapiv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: GatewayClassName,
		},
		Spec: gwapiv1.GatewayClassSpec{
			ControllerName: gwapiv1.GatewayController(GatewayControllerName),
		},
	}

	return rr.AddResources(gatewayClass)
}

func handleCertificates(ctx context.Context, rr *odhtypes.ReconciliationRequest, gatewayConfig *serviceApi.GatewayConfig, domain string) (string, error) {
	// Input validation
	if gatewayConfig == nil {
		return "", errors.New("gatewayConfig cannot be nil")
	}
	if domain == "" {
		return "", errors.New("domain cannot be empty")
	}

	// Get certificate configuration with default fallback
	certConfig := gatewayConfig.Spec.Certificate
	if certConfig == nil {
		certConfig = &infrav1.CertificateSpec{
			Type: infrav1.OpenshiftDefaultIngress,
		}
	}

	// Generate secret name with fallback
	secretName := certConfig.SecretName
	if secretName == "" {
		secretName = fmt.Sprintf("%s-tls", gatewayConfig.Name)
	}

	switch certConfig.Type {
	case infrav1.OpenshiftDefaultIngress:
		return handleOpenshiftDefaultCertificate(ctx, rr, secretName)
	case infrav1.SelfSigned:
		return handleSelfSignedCertificate(ctx, rr, secretName, domain)
	case infrav1.Provided:
		return secretName, nil
	default:
		return "", fmt.Errorf("unsupported certificate type: %s", certConfig.Type)
	}
}

func handleOpenshiftDefaultCertificate(ctx context.Context, rr *odhtypes.ReconciliationRequest, secretName string) (string, error) {
	err := cluster.PropagateDefaultIngressCertificate(ctx, rr.Client, secretName, GatewayNamespace)
	if err != nil {
		return "", fmt.Errorf("failed to propagate default ingress certificate: %w", err)
	}

	return secretName, nil
}

func handleSelfSignedCertificate(ctx context.Context, rr *odhtypes.ReconciliationRequest, secretName string, domain string) (string, error) {
	err := cluster.CreateSelfSignedCertificate(
		ctx,
		rr.Client,
		secretName,
		domain,
		GatewayNamespace,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create self-signed certificate: %w", err)
	}

	return secretName, nil
}

func createGateway(rr *odhtypes.ReconciliationRequest, certSecretName string, domain string, gatewayName string) error {
	// Input validation
	if rr == nil {
		return errors.New("reconciliation request cannot be nil")
	}
	if gatewayName == "" {
		return errors.New("gateway name cannot be empty")
	}
	if domain == "" {
		return errors.New("domain cannot be empty")
	}

	// Create listeners with namespace restrictions
	listeners := createListeners(certSecretName, domain)

	// Create gateway resource with optimized structure
	gateway := &gwapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gatewayName,
			Namespace: GatewayNamespace,
		},
		Spec: gwapiv1.GatewaySpec{
			GatewayClassName: gwapiv1.ObjectName(GatewayClassName),
			Listeners:        listeners,
		},
	}

	return rr.AddResources(gateway)
}

// detectClusterAuthMode determines the authentication mode from cluster configuration.
func detectClusterAuthMode(ctx context.Context, rr *odhtypes.ReconciliationRequest) (AuthMode, error) {
	auth := &configv1.Authentication{}
	err := rr.Client.Get(ctx, types.NamespacedName{Name: "cluster"}, auth)
	if err != nil {
		return "", fmt.Errorf("failed to get cluster authentication config: %w", err)
	}

	switch auth.Spec.Type {
	case "OIDC":
		return AuthModeOIDC, nil
	case "IntegratedOAuth", "":
		// empty string is equivalent to IntegratedOAuth (default)
		return AuthModeIntegratedOAuth, nil
	case "None":
		return AuthModeNone, nil
	default:
		return AuthModeIntegratedOAuth, nil
	}
}

func validateOIDCConfig(authMode AuthMode, oidcConfig *serviceApi.OIDCConfig) *common.Condition {
	if authMode != AuthModeOIDC {
		return nil
	}

	condition := &common.Condition{
		Type:   status.ConditionTypeReady,
		Status: metav1.ConditionFalse,
		Reason: status.NotReadyReason,
	}

	if oidcConfig == nil {
		condition.Message = status.AuthProxyOIDCModeWithoutConfigMessage
		return condition
	}

	var validationErrors []string

	if oidcConfig.ClientID == "" {
		validationErrors = append(validationErrors, status.AuthProxyOIDCClientIDEmptyMessage)
	}
	if oidcConfig.IssuerURL == "" {
		validationErrors = append(validationErrors, status.AuthProxyOIDCIssuerURLEmptyMessage)
	}
	if oidcConfig.ClientSecretRef.Name == "" {
		validationErrors = append(validationErrors, status.AuthProxyOIDCSecretRefNameEmptyMessage)
	}

	if len(validationErrors) > 0 {
		condition.Message = strings.Join(validationErrors, ", ")
		return condition
	}

	return nil
}

func checkAuthModeNone(authMode AuthMode) *common.Condition {
	if authMode == AuthModeNone {
		return &common.Condition{
			Type:    status.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  status.NotReadyReason,
			Message: "Cluster uses external authentication, no gateway auth proxy deployed",
		}
	}
	return nil
}

// getOrGenerateSecrets retrieves existing secrets or generates new ones for OAuth2 proxy.
func getOrGenerateSecrets(ctx context.Context, rr *odhtypes.ReconciliationRequest, authMode AuthMode) (string, string, error) {
	existingSecret := &corev1.Secret{}
	secretErr := rr.Client.Get(ctx, types.NamespacedName{
		Name:      KubeAuthProxySecretsName,
		Namespace: GatewayNamespace,
	}, existingSecret)

	if secretErr == nil {
		clientSecretBytes, hasClientSecret := existingSecret.Data[EnvClientSecret]
		cookieSecretBytes, hasCookieSecret := existingSecret.Data[EnvCookieSecret]

		if !hasClientSecret || !hasCookieSecret {
			return "", "", errors.New("existing secret missing required keys")
		}

		return string(clientSecretBytes), string(cookieSecretBytes), nil
	}

	if !k8serr.IsNotFound(secretErr) {
		return "", "", fmt.Errorf("failed to check for existing secret: %w", secretErr)
	}

	var clientSecretValue string
	if authMode == AuthModeIntegratedOAuth {
		clientSecretGen, err := cluster.NewSecret("client-secret", "random", ClientSecretLength)
		if err != nil {
			return "", "", fmt.Errorf("failed to generate client secret: %w", err)
		}
		clientSecretValue = clientSecretGen.Value
	}

	cookieSecretGen, err := cluster.NewSecret("cookie-secret", "random", CookieSecretLength)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate cookie secret: %w", err)
	}

	return clientSecretValue, cookieSecretGen.Value, nil
}

// createSecretKeySelector creates a standard secret key selector for OAuth2 proxy environment variables.
func createSecretKeySelector(key string) *corev1.EnvVarSource {
	return &corev1.EnvVarSource{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: KubeAuthProxySecretsName,
			},
			Key: key,
		},
	}
}

// This helper reduces code duplication and improves error handling consistency.
func validateGatewayConfig(rr *odhtypes.ReconciliationRequest) (*serviceApi.GatewayConfig, error) {
	if rr == nil {
		return nil, errors.New("reconciliation request cannot be nil")
	}
	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return nil, errors.New("instance is not of type *services.GatewayConfig")
	}
	if gatewayConfig == nil {
		return nil, errors.New("gatewayConfig cannot be nil")
	}
	return gatewayConfig, nil
}

// deployKubeAuthProxy deploys the complete OAuth2 proxy infrastructure including secret, service and deployment.
func deployKubeAuthProxy(ctx context.Context, rr *odhtypes.ReconciliationRequest,
	oidcConfig *serviceApi.OIDCConfig, cookieConfig *serviceApi.CookieConfig,
	clientSecret, cookieSecret string, domain string) error {
	l := logf.FromContext(ctx).WithName("deployAuthProxy")

	if oidcConfig != nil {
		l.V(1).Info("configuring kube-auth-proxy for external OIDC",
			"issuerURL", oidcConfig.IssuerURL,
			"clientID", oidcConfig.ClientID,
			"secretRef", oidcConfig.ClientSecretRef.Name)
	} else {
		l.V(1).Info("configuring kube-auth-proxy for OpenShift OAuth")
	}

	err := createKubeAuthProxySecret(ctx, rr, clientSecret, cookieSecret, oidcConfig)
	if err != nil {
		return err
	}

	l.V(1).Info("secret created, proceeding with dependent resources", "secret", "kube-auth-proxy-creds")

	err = createKubeAuthProxyService(rr)
	if err != nil {
		return err
	}

	err = createKubeAuthProxyDeployment(ctx, rr, oidcConfig, cookieConfig, domain)
	if err != nil {
		return err
	}

	return nil
}

// getOIDCClientSecret retrieves the client secret from the referenced secret for OIDC configuration.
func getOIDCClientSecret(ctx context.Context, client client.Client, oidcConfig *serviceApi.OIDCConfig) (string, error) {
	secret := &corev1.Secret{}
	err := client.Get(ctx, types.NamespacedName{
		Name:      oidcConfig.ClientSecretRef.Name,
		Namespace: GatewayNamespace,
	}, secret)
	if err != nil {
		return "", fmt.Errorf("failed to get OIDC client secret %s/%s: %w",
			GatewayNamespace, oidcConfig.ClientSecretRef.Name, err)
	}

	key := oidcConfig.ClientSecretRef.Key
	if key == "" {
		key = DefaultClientSecretKey
	}

	if secretValue, exists := secret.Data[key]; exists {
		return string(secretValue), nil
	}

	return "", fmt.Errorf("key '%s' not found in secret %s/%s",
		key, GatewayNamespace, oidcConfig.ClientSecretRef.Name)
}

func createKubeAuthProxySecret(ctx context.Context, rr *odhtypes.ReconciliationRequest, clientSecret, cookieSecret string, oidcConfig *serviceApi.OIDCConfig) error {
	clientId := AuthClientID
	clientSecretValue := clientSecret

	if oidcConfig != nil {
		clientId = oidcConfig.ClientID
		var err error
		clientSecretValue, err = getOIDCClientSecret(ctx, rr.Client, oidcConfig)
		if err != nil {
			return err
		}
	}

	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      KubeAuthProxySecretsName,
			Namespace: GatewayNamespace,
			Labels:    KubeAuthProxyLabels,
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			EnvClientID:     clientId,
			EnvClientSecret: clientSecretValue,
			EnvCookieSecret: cookieSecret,
		},
	}

	opts := []client.PatchOption{
		client.ForceOwnership,
		client.FieldOwner(resources.PlatformFieldOwner),
	}
	err := resources.Apply(ctx, rr.Client, secret, opts...)
	if err != nil && !k8serr.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func createKubeAuthProxyDeployment(
	ctx context.Context, rr *odhtypes.ReconciliationRequest,
	oidcConfig *serviceApi.OIDCConfig,
	cookieConfig *serviceApi.CookieConfig,
	domain string) error {
	// secret doesn't exist use empty string.
	secret := &corev1.Secret{}
	secretHash := ""
	err := rr.Client.Get(ctx, types.NamespacedName{
		Name:      KubeAuthProxySecretsName,
		Namespace: GatewayNamespace,
	}, secret)
	if err == nil {
		// Secret exists, calculate its hash
		secretHash = calculateSecretHash(secret.Data)
	} else if !k8serr.IsNotFound(err) {
		return fmt.Errorf("failed to get secret: %w", err)
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KubeAuthProxyName,
			Namespace: GatewayNamespace,
			Labels:    KubeAuthProxyLabels,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: KubeAuthProxyLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: KubeAuthProxyLabels,
					Annotations: map[string]string{
						"opendatahub.io/secret-hash": secretHash,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  KubeAuthProxyName,
							Image: getKubeAuthProxyImage(),
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: AuthProxyHTTPPort,
									Name:          "http",
								},
								{
									ContainerPort: AuthProxyHTTPSPort,
									Name:          "https",
								},
								{
									ContainerPort: AuthProxyMetricsPort,
									Name:          "metrics",
								},
							},
							Args: buildOAuth2ProxyArgs(oidcConfig, cookieConfig, domain),
							Env: []corev1.EnvVar{
								{Name: EnvClientID, ValueFrom: createSecretKeySelector(EnvClientID)},
								{Name: EnvClientSecret, ValueFrom: createSecretKeySelector(EnvClientSecret)},
								{Name: EnvCookieSecret, ValueFrom: createSecretKeySelector(EnvCookieSecret)},
								{Name: "PROXY_MODE", Value: "auth"},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      TLSCertsVolumeName,
									MountPath: TLSCertsMountPath,
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: TLSCertsVolumeName,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: KubeAuthProxyTLSName,
								},
							},
						},
					},
				},
			},
		},
	}

	return rr.AddResources(deployment)
}

func buildOAuth2ProxyArgs(oidcConfig *serviceApi.OIDCConfig, cookieConfig *serviceApi.CookieConfig, domain string) []string {
	// OAuth2 proxy acts as auth service only - no upstream needed
	baseArgs := buildBaseOAuth2ProxyArgs(cookieConfig, domain)

	if oidcConfig != nil {
		return append(baseArgs, buildOIDCArgs(oidcConfig)...)
	}

	return append(baseArgs, buildOpenShiftOAuthArgs()...)
}

// getCookieSettings returns cookie expire and refresh durations with defaults.
func getCookieSettings(cookieConfig *serviceApi.CookieConfig) (string, string) {
	// Set defaults
	expire, refresh := "24h", "1h"

	// Override with user configuration if provided
	if cookieConfig != nil {
		if cookieConfig.Expire != "" {
			expire = cookieConfig.Expire
		}
		if cookieConfig.Refresh != "" {
			refresh = cookieConfig.Refresh
		}
	}

	return expire, refresh
}

func buildBaseOAuth2ProxyArgs(cookieConfig *serviceApi.CookieConfig, domain string) []string {
	cookieExpire, cookieRefresh := getCookieSettings(cookieConfig)

	return []string{
		fmt.Sprintf("--http-address=0.0.0.0:%d", AuthProxyHTTPPort),
		"--email-domain=*",
		"--upstream=static://200", // Static response - real routing handled by EnvoyFilter
		"--skip-provider-button",
		"--skip-jwt-bearer-tokens=true", // Allow bearer tokens to bypass OAuth login flow
		"--pass-access-token=true",
		"--set-xauthrequest=true",
		fmt.Sprintf("--redirect-url=https://%s/oauth2/callback", domain),
		"--tls-cert-file=" + TLSCertsMountPath + "/tls.crt",
		"--tls-key-file=" + TLSCertsMountPath + "/tls.key",
		"--use-system-trust-store=true",
		fmt.Sprintf("--https-address=0.0.0.0:%d", AuthProxyHTTPSPort),
		"--cookie-expire=" + cookieExpire,                                 // Configurable cookie expiration
		"--cookie-refresh=" + cookieRefresh,                               // Configurable cookie refresh interval
		"--cookie-secure=true",                                            // HTTPS only
		"--cookie-httponly=true",                                          // XSS protection
		"--cookie-samesite=lax",                                           // CSRF protection
		fmt.Sprintf("--cookie-name=%s", OAuth2ProxyCookieName),            // Custom cookie name (used in EnvoyFilter Lua filter)
		"--cookie-domain=" + domain,                                       // Cookie domain is the domain of the gateway
		fmt.Sprintf("--metrics-address=0.0.0.0:%d", AuthProxyMetricsPort), // Expose metrics on unauthenticated port
	}
}

func buildOIDCArgs(oidcConfig *serviceApi.OIDCConfig) []string {
	return []string{
		"--provider=oidc",
		"--oidc-issuer-url=" + oidcConfig.IssuerURL,
		"--skip-oidc-discovery=false", // Enable OIDC discovery
	}
}

func buildOpenShiftOAuthArgs() []string {
	return []string{
		"--provider=openshift",
		"--scope=" + OpenShiftOAuthScope,
	}
}

func createKubeAuthProxyService(rr *odhtypes.ReconciliationRequest) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KubeAuthProxyName,
			Namespace: GatewayNamespace,
			Labels:    KubeAuthProxyLabels,
			Annotations: map[string]string{
				"service.beta.openshift.io/serving-cert-secret-name": KubeAuthProxyTLSName,
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: KubeAuthProxyLabels,
			Ports: []corev1.ServicePort{
				{
					Name:       "https",
					Port:       AuthProxyHTTPSPort,
					TargetPort: intstr.FromInt(AuthProxyHTTPSPort),
				},
				{
					Name:       "metrics",
					Port:       AuthProxyMetricsPort,
					TargetPort: intstr.FromInt(AuthProxyMetricsPort),
				},
			},
		},
	}

	return rr.AddResources(service)
}

// createOAuthClient creates an OpenShift OAuth client for integrated authentication.
func createOAuthClient(ctx context.Context, rr *odhtypes.ReconciliationRequest, clientSecret string) error {
	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return errors.New("instance is not of type *services.GatewayConfig")
	}

	// Use consistent domain resolution with the gateway
	domain, err := resolveDomain(ctx, rr.Client, gatewayConfig)
	if err != nil {
		return fmt.Errorf("failed to resolve domain: %w", err)
	}

	oauthClient := &oauthv1.OAuthClient{
		ObjectMeta: metav1.ObjectMeta{
			Name: AuthClientID,
		},
		GrantMethod:  oauthv1.GrantHandlerAuto,
		RedirectURIs: []string{fmt.Sprintf("https://%s/oauth2/callback", domain)},
		Secret:       clientSecret,
	}

	return rr.AddResources(oauthClient)
}

// createHTTPRoute creates a common HTTPRoute with optional URL rewrite filter.
func createHTTPRoute(routeName, path, serviceName, serviceNamespace string, port int32, urlRewrite *gwapiv1.HTTPURLRewriteFilter) *gwapiv1.HTTPRoute {
	pathPrefix := gwapiv1.PathMatchPathPrefix
	gatewayNS := gwapiv1.Namespace(GatewayNamespace)
	servicePort := gwapiv1.PortNumber(port)

	rule := gwapiv1.HTTPRouteRule{
		Matches: []gwapiv1.HTTPRouteMatch{
			{
				Path: &gwapiv1.HTTPPathMatch{
					Type:  &pathPrefix,
					Value: &path,
				},
			},
		},
		BackendRefs: []gwapiv1.HTTPBackendRef{
			{
				BackendRef: gwapiv1.BackendRef{
					BackendObjectReference: gwapiv1.BackendObjectReference{
						Name:      gwapiv1.ObjectName(serviceName),
						Namespace: (*gwapiv1.Namespace)(&serviceNamespace),
						Port:      &servicePort,
					},
				},
			},
		},
	}

	// Add URL rewrite filter if provided
	if urlRewrite != nil {
		rule.Filters = []gwapiv1.HTTPRouteFilter{
			{
				Type:       gwapiv1.HTTPRouteFilterURLRewrite,
				URLRewrite: urlRewrite,
			},
		}
	}

	return &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: GatewayNamespace,
		},
		Spec: gwapiv1.HTTPRouteSpec{
			CommonRouteSpec: gwapiv1.CommonRouteSpec{
				ParentRefs: []gwapiv1.ParentReference{
					{
						Name:      gwapiv1.ObjectName(DefaultGatewayName),
						Namespace: &gatewayNS,
					},
				},
			},
			Rules: []gwapiv1.HTTPRouteRule{rule},
		},
	}
}

func createOAuthCallbackRoute(rr *odhtypes.ReconciliationRequest) error {
	httpRoute := createHTTPRoute(
		OAuthCallbackRouteName,
		AuthProxyOAuth2Path,
		KubeAuthProxyName,
		GatewayNamespace,
		AuthProxyHTTPSPort,
		nil, // no URL rewrite for OAuth callback
	)
	return rr.AddResources(httpRoute)
}

// isIngressCertificateSecret returns true if obj is the certificate secret used by the default IngressController.
func isIngressCertificateSecret(ctx context.Context, cli client.Client, obj client.Object) bool {
	if obj.GetNamespace() != cluster.IngressNamespace {
		return false
	}

	ingressCtrl, err := cluster.FindAvailableIngressController(ctx, cli)
	if err != nil {
		return false
	}

	ingressCertName := cluster.GetDefaultIngressCertSecretName(ingressCtrl)
	return obj.GetName() == ingressCertName
}
