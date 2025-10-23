package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	oauthv1 "github.com/openshift/api/oauth/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/secretgenerator"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// AuthMode represents different authentication modes supported by the gateway.
type AuthMode string

const (
	AuthModeIntegratedOAuth AuthMode = "IntegratedOAuth"
	AuthModeOIDC            AuthMode = "OIDC"
	AuthModeNone            AuthMode = "None"
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

func createKubeAuthProxyInfrastructure(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createAuthProxy")

	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return errors.New("instance is not of type *services.GatewayConfig")
	}

	l.V(1).Info("creating auth proxy for gateway", "gateway", gatewayConfig.Name)

	// Resolve domain consistently with createGatewayInfrastructure
	domain, err := ResolveDomain(ctx, rr.Client, gatewayConfig)
	if err != nil {
		return fmt.Errorf("failed to resolve domain: %w", err)
	}

	authMode, err := detectClusterAuthMode(ctx, rr)
	if err != nil {
		return fmt.Errorf("failed to detect cluster authentication mode: %w", err)
	}

	if errorCondition := validateOIDCConfig(authMode, gatewayConfig.Spec.OIDC); errorCondition != nil {
		return fmt.Errorf("%s", errorCondition.Message)
	}

	if condition := checkAuthModeNone(authMode); condition != nil {
		return fmt.Errorf("%s", condition.Message)
	}

	var oidcConfig *serviceApi.OIDCConfig
	if authMode == AuthModeOIDC {
		oidcConfig = gatewayConfig.Spec.OIDC
	}

	// get or generate secrets for kube-auth-proxy (handles OAuth and OIDC modes)
	clientSecret, cookieSecret, err := getOrGenerateSecrets(ctx, rr, authMode)
	if err != nil {
		return fmt.Errorf("failed to get or generate secrets: %w", err)
	}

	if err := deployKubeAuthProxy(ctx, rr, oidcConfig, gatewayConfig.Spec.Cookie, clientSecret, cookieSecret, domain); err != nil {
		return fmt.Errorf("failed to deploy auth proxy: %w", err)
	}

	if authMode == AuthModeIntegratedOAuth {
		if err := createOAuthClient(ctx, rr, clientSecret); err != nil {
			return fmt.Errorf("failed to create OAuth client: %w", err)
		}
	}

	if err := createOAuthCallbackRoute(rr); err != nil {
		return fmt.Errorf("failed to create OAuth callback route: %w", err)
	}

	return nil
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
		clientSecretGen, err := secretgenerator.NewSecret("client-secret", "random", ClientSecretLength)
		if err != nil {
			return "", "", fmt.Errorf("failed to generate client secret: %w", err)
		}
		clientSecretValue = clientSecretGen.Value
	}

	cookieSecretGen, err := secretgenerator.NewSecret("cookie-secret", "random", CookieSecretLength)
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
		"--cookie-name=_oauth2_proxy",                                     // Custom cookie name
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

func createEnvoyFilter(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	// using yaml templates due to complexity of k8s api struct for envoy filter
	yamlContent, err := gatewayResources.ReadFile("resources/envoyfilter-authn.yaml")
	if err != nil {
		return fmt.Errorf("failed to read EnvoyFilter template: %w", err)
	}

	decoder := serializer.NewCodecFactory(rr.Client.Scheme()).UniversalDeserializer()
	unstructuredObjects, err := resources.Decode(decoder, yamlContent)
	if err != nil {
		return fmt.Errorf("failed to decode EnvoyFilter YAML: %w", err)
	}

	if len(unstructuredObjects) != 1 {
		return fmt.Errorf("expected exactly 1 EnvoyFilter object, got %d", len(unstructuredObjects))
	}

	return rr.AddResources(&unstructuredObjects[0])
}

// createOAuthClient creates an OpenShift OAuth client for integrated authentication.
func createOAuthClient(ctx context.Context, rr *odhtypes.ReconciliationRequest, clientSecret string) error {
	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return errors.New("instance is not of type *services.GatewayConfig")
	}

	// Use consistent domain resolution with the gateway
	domain, err := ResolveDomain(ctx, rr.Client, gatewayConfig)
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
