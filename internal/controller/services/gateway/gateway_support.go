package gateway

import (
	"context"
	"errors"
	"fmt"

	oauthv1 "github.com/openshift/api/oauth/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/secretgenerator"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

func createGatewayClass(rr *odhtypes.ReconciliationRequest) error {
	gatewayClass := &gwapiv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: GatewayClassName,
		},
		Spec: gwapiv1.GatewayClassSpec{
			ControllerName: "openshift.io/gateway-controller/v1",
		},
	}

	return rr.AddResources(gatewayClass)
}

func getCertificateType(gatewayConfig *serviceApi.GatewayConfig) string {
	certType := gatewayConfig.Spec.IngressGateway.Certificate.Type
	if certType == "" { // zero value
		return string(infrav1.OpenshiftDefaultIngress)
	}
	return string(certType)
}

// It first checks if a domain is specified in the GatewayConfig spec,
// and falls back to from the cluster domain.
func getDomain(ctx context.Context, rr *odhtypes.ReconciliationRequest, gatewayConfig *serviceApi.GatewayConfig) (string, error) {
	domain := gatewayConfig.Spec.IngressGateway.Domain
	if domain == "" {
		clusterDomain, err := cluster.GetDomain(ctx, rr.Client)
		if err != nil {
			return "", fmt.Errorf("failed to get cluster domain: %w", err) // TODO: check, old logic was to use cluster.local in this case.
		}
		domain = fmt.Sprintf("%s.%s", GatewayName, clusterDomain)
	}
	return domain, nil
}

func createGateway(rr *odhtypes.ReconciliationRequest, certSecretName string, domain string) error {
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

	gateway := &gwapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GatewayName,
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
	certConfig := gatewayConfig.Spec.IngressGateway.Certificate

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
		hostname := fmt.Sprintf("%s.%s", GatewayName, domain)
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
			Name:      KubeAuthProxyCredsSecret,
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

func createOAuthClient(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	// Read client secret from kube-auth-proxy-creds secret
	authSecret := &corev1.Secret{}
	if err := rr.Client.Get(ctx, types.NamespacedName{
		Name:      KubeAuthProxyCredsSecret,
		Namespace: GatewayNamespace,
	}, authSecret); err != nil {
		return fmt.Errorf("failed to get auth proxy secret %s/%s: %w", GatewayNamespace, KubeAuthProxyCredsSecret, err)
	}

	clientSecretBytes, exists := authSecret.Data["OAUTH2_PROXY_CLIENT_SECRET"]
	if !exists {
		return fmt.Errorf("OAUTH2_PROXY_CLIENT_SECRET not found in secret %s/%s", GatewayNamespace, KubeAuthProxyCredsSecret)
	}
	clientSecret := string(clientSecretBytes)

	clusterDomain, err := cluster.GetDomain(ctx, rr.Client)
	if err != nil {
		return fmt.Errorf("failed to get cluster domain: %w", err)
	}

	redirectURL := fmt.Sprintf("https://%s.%s/oauth2/callback", GatewayName, clusterDomain)

	oauthClient := &oauthv1.OAuthClient{
		ObjectMeta: metav1.ObjectMeta{
			Name: AuthClientID,
		},
		GrantMethod:  oauthv1.GrantHandlerAuto,
		RedirectURIs: []string{redirectURL},
		Secret:       clientSecret,
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
		Name:      KubeAuthProxyCredsSecret,
		Namespace: GatewayNamespace,
	}, existingSecret)

	// Fast exit on NotFound errors
	if secretErr != nil && !k8serr.IsNotFound(secretErr) {
		return "", "", "", fmt.Errorf("failed to check existing secret %s/%s: %w", GatewayNamespace, KubeAuthProxyCredsSecret, secretErr)
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

		externalSecret := &corev1.Secret{}
		if err := rr.Client.Get(ctx, types.NamespacedName{
			Name:      oidcConfig.ClientSecretRef.Name,
			Namespace: GatewayNamespace,
		}, externalSecret); err != nil {
			return "", "", "", fmt.Errorf("failed to get OIDC client secret %s/%s: %w",
				GatewayNamespace, oidcConfig.ClientSecretRef.Name, err)
		}

		key := oidcConfig.ClientSecretRef.Key
		if key == "" {
			key = "clientSecret"
		}

		if secretValue, exists := externalSecret.Data[key]; exists {
			clientSecretValue = string(secretValue)
		} else {
			return "", "", "", fmt.Errorf("key '%s' not found in OIDC secret %s/%s", key, GatewayNamespace, oidcConfig.ClientSecretRef.Name)
		}

	case cluster.AuthModeIntegratedOAuth:
		// OAuth mode: generate new client secret
		clientID = AuthClientID

		clientSecretGen, err := secretgenerator.NewSecret("client-secret", "random", 24)
		if err != nil {
			return "", "", "", fmt.Errorf("failed to generate client secret: %w", err)
		}
		clientSecretValue = clientSecretGen.Value

	default:
		return "", "", "", fmt.Errorf("auth mode: %s is not supported", authMode)
	}

	// Always generate new cookie secret on oauth or oidc mode.
	cookieSecretGen, err := secretgenerator.NewSecret("cookie-secret", "random", 32)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to generate cookie secret: %w", err)
	}

	return clientID, clientSecretValue, cookieSecretGen.Value, nil
}
