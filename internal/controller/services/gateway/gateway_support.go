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
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/gatewayconfig"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	ServiceName = serviceApi.GatewayServiceName
	// ReadyConditionType is GatewayConfig's service-specific Ready condition; the reconciler computes aggregate Ready.
	ReadyConditionType = serviceApi.GatewayConfigKind + status.ReadySuffix
)

// Configuration failures affect one target; client failures must stop before deployment and GC.
var errInvalidAuthenticationConfig = errors.New("invalid authentication configuration")

const (
	// Gateway infrastructure constants.
	GatewayNamespace           = "openshift-ingress"                  // Namespace where Gateway resources are deployed on OpenShift
	XKSGatewayNamespace        = "rh-ai-gateway"                      // Namespace for gateway resources on XKS (vanilla K8s)
	GatewayClassName           = serviceApi.GatewayClassName          // GatewayClass name for gateway resources
	GatewayControllerName      = "openshift.io/gateway-controller/v1" // OpenShift Gateway API controller name
	XKSGatewayControllerName   = "istio.io/gateway-controller"        // XKS (standard Istio) Gateway API controller name
	DefaultGatewayName         = serviceApi.DefaultGatewayName        // Default gateway resource name on OpenShift
	XKSDefaultGatewayName      = serviceApi.XKSDefaultGatewayName     // Default gateway resource name on XKS
	HTTPSPortName              = serviceApi.DefaultGatewayListenerName
	DefaultGatewayListenerName = serviceApi.DefaultGatewayListenerName
	LegacyGatewayListenerName  = serviceApi.LegacyGatewayListenerName
	DefaultGatewaySubdomain    = "rh-ai"                // Default subdomain for gateway URLs
	LegacyGatewaySubdomain     = "data-science-gateway" // Legacy subdomain to redirect from
	XKSIstioRevisionValue      = "default"              // Istio revision label value on XKS

	// Authentication constants.
	LegacyAuthClientID       = "odh"          // Legacy OauthClient name from RHOAI 3.3.
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
	OAuthCallbackPath   = AuthProxyOAuth2Path + "/callback"
	// OAuth2 proxy cookie name - used in both proxy args and EnvoyFilter Lua filter.
	AuthProxyCookieName = "_oauth2_proxy"

	// Volume and mount paths.
	TLSCertsVolumeName = "tls-certs"
	TLSCertsMountPath  = "/etc/tls/private"

	// Secret configuration.
	DefaultGatewayTLSSecretName = "data-science-gatewayconfig-tls"

	// Gateway infrastructure configuration.
	GatewayInfraConfigMapName   = "data-science-gateway-config"
	GatewayServiceTLSSecretName = "data-science-gateway-service-tls"
	IstioRevisionLabel          = "istio.io/rev"
	IstioRevisionValue          = "openshift-gateway"

	// Environment variable names for OAuth2 proxy.
	EnvClientID     = "OAUTH2_PROXY_CLIENT_ID"
	EnvClientSecret = "OAUTH2_PROXY_CLIENT_SECRET" //nolint:gosec // This is an environment variable name, not a secret
	EnvCookieSecret = "OAUTH2_PROXY_COOKIE_SECRET" //nolint:gosec // This is an environment variable name, not a secret

	// Dashboard redirect constants.
	DashboardRedirectName       = "dashboard-redirect"
	DashboardRedirectConfigName = "dashboard-redirect-config"
	DashboardRouteNameODH       = "odh-dashboard"   // ODH dashboard route name
	DashboardRouteNameRHOAI     = "rhods-dashboard" // RHOAI dashboard route name

	// Label constants.
	ComponentLabelValue = "authentication"
	PartOfLabelValue    = "data-science-gateway"
	PartOfGatewayConfig = "gatewayconfig"
)

const (
	envoyFilterTemplate                     = "resources/envoyfilter-authn.tmpl.yaml"
	destinationRuleTemplate                 = "resources/kube-auth-proxy-destinationrule-tls.tmpl.yaml"
	kubeAuthProxyDeploymentOidcTemplate     = "resources/kube-auth-proxy-oidc-deployment.tmpl.yaml"
	kubeAuthProxyDeploymentOauthTemplate    = "resources/kube-auth-proxy-oauth-deployment.tmpl.yaml"
	kubeAuthProxyServiceTemplate            = "resources/kube-auth-proxy-svc.tmpl.yaml"
	kubeAuthProxyHTTPRouteTemplate          = "resources/kube-auth-proxy-httproute.tmpl.yaml"
	kubeAuthProxyHPATemplate                = "resources/kube-auth-proxy-hpa.tmpl.yaml"
	kubeAuthProxyServiceAccountTemplate     = "resources/kube-auth-proxy-serviceaccount.tmpl.yaml"
	kubeAuthProxyClusterRoleBindingTemplate = "resources/kube-auth-proxy-clusterrolebinding.tmpl.yaml"
	networkPolicyTemplate                   = "resources/kube-auth-proxy-networkpolicy.yaml"
	ocpRouteTemplate                        = "resources/gateway-ocp-route.tmpl.yaml"
)

// GetGatewayNamespace returns the namespace where gateway resources are deployed.
// On OpenShift: "openshift-ingress" (fixed by the platform).
// On XKS (vanilla K8s): "rh-ai-gateway".
func GetGatewayNamespace() string {
	if cluster.GetClusterInfo().Type == cluster.ClusterTypeKubernetes {
		return XKSGatewayNamespace
	}
	return GatewayNamespace
}

// GetDefaultGatewayName returns the default gateway CR name.
// On OpenShift: "data-science-gateway".
// On XKS: "rh-ai-gateway".
func GetDefaultGatewayName() string {
	if cluster.GetClusterInfo().Type == cluster.ClusterTypeKubernetes {
		return XKSDefaultGatewayName
	}
	return DefaultGatewayName
}

// GetGatewayControllerName returns the GatewayClass controller name.
// On OpenShift: "openshift.io/gateway-controller/v1".
// On XKS: "istio.io/gateway-controller" (standard Istio controller).
func GetGatewayControllerName() gwapiv1.GatewayController {
	if cluster.GetClusterInfo().Type == cluster.ClusterTypeKubernetes {
		return XKSGatewayControllerName
	}
	return GatewayControllerName
}

// GetIstioRevisionValue returns the Istio revision label value.
// On OpenShift: "openshift-gateway" (managed by OpenShift Service Mesh).
// On XKS: "default" (standard Istio installation via odh-gitops sail chart).
func GetIstioRevisionValue() string {
	if cluster.GetClusterInfo().Type == cluster.ClusterTypeKubernetes {
		return XKSIstioRevisionValue
	}
	return IstioRevisionValue
}

// GetDefaultGatewayServiceFullName returns the auto-created Gateway service name.
// Format: <gateway-name>-<gatewayclass-name>.
func GetDefaultGatewayServiceFullName() string {
	return GetGatewayServiceFullName(GetDefaultGatewayName())
}

// GetGatewayServiceFullName returns Istio's Service name for a Gateway.
func GetGatewayServiceFullName(gatewayName string) string {
	return gatewayName + "-" + GatewayClassName
}

func additionalGatewayInfrastructureConfigMapName(ingressName string) string {
	return ingressName + "-config"
}

func additionalGatewayServiceTLSSecretName(ingressName string) string {
	return ingressName + "-service-tls"
}

func additionalGatewayEnvoyFilterName(ingressName string) string {
	return ingressName + "-authn"
}

type gatewayEnvoyFilterTarget struct {
	Name        string
	GatewayName string
}

// ErrDomainRequired is returned by GetFQDN when spec.domain is not set on non-OpenShift clusters.
var ErrDomainRequired = errors.New("spec.domain is required on non-OpenShift clusters")

// GetFQDN returns the fully qualified domain name for the gateway based on the GatewayConfig.
// It constructs the FQDN by combining the subdomain (or default) with either the user-specified
// domain or the cluster domain.
func GetFQDN(ctx context.Context, cli client.Client, gatewayConfig *serviceApi.GatewayConfig) (string, error) {
	subdomain := getCurrentSubdomain(gatewayConfig)

	if gatewayConfig != nil {
		baseDomain := strings.TrimSpace(gatewayConfig.Spec.Domain)
		if baseDomain != "" {
			return fmt.Sprintf("%s.%s", subdomain, baseDomain), nil
		}
	}

	// On XKS, domain must be explicitly set in GatewayConfig (no OpenShift Ingress CR to auto-detect)
	if cluster.GetClusterInfo().Type == cluster.ClusterTypeKubernetes {
		return "", ErrDomainRequired
	}

	clusterDomain, err := cluster.GetDomain(ctx, cli)
	if err != nil {
		return "", fmt.Errorf("failed to get cluster domain: %w", err)
	}

	return fmt.Sprintf("%s.%s", subdomain, clusterDomain), nil
}

// resolveGatewayHostname returns the gateway FQDN. When spec.domain is required but not set
// on XKS, it sets GatewayConfigReady=False and returns ErrDomainRequired so callers can
// return nil without propagating an error (matching the missing-OIDC configuration pattern).
func resolveGatewayHostname(
	ctx context.Context,
	rr *odhtypes.ReconciliationRequest,
	gatewayConfig *serviceApi.GatewayConfig,
) (string, error) {
	hostname, err := GetFQDN(ctx, rr.Client, gatewayConfig)
	if err == nil {
		return hostname, nil
	}
	if errors.Is(err, ErrDomainRequired) {
		rr.Conditions.MarkFalse(
			ReadyConditionType,
			conditions.WithReason(status.NotReadyReason),
			conditions.WithMessage(status.GatewayDomainRequiredMessage),
		)
		return "", ErrDomainRequired
	}
	return "", fmt.Errorf("failed to resolve domain: %w", err)
}

// isGatewayDomainUnresolved reports whether gateway hostname resolution should stop
// downstream reconciliation actions without error.
func isGatewayDomainUnresolved(
	ctx context.Context,
	rr *odhtypes.ReconciliationRequest,
	gatewayConfig *serviceApi.GatewayConfig,
) (bool, error) {
	_, err := resolveGatewayHostname(ctx, rr, gatewayConfig)
	if errors.Is(err, ErrDomainRequired) {
		return true, nil
	}
	return err != nil, err
}

// GetGatewayDomain reads the domain directly from the Gateway CR's listener hostname.
// Falls back to GatewayConfig if Gateway CR doesn't exist yet.
func GetGatewayDomain(ctx context.Context, cli client.Client) (string, error) {
	// Try to get the Gateway CR first
	gateway := &gwapiv1.Gateway{}
	err := cli.Get(ctx, client.ObjectKey{
		Name:      GetDefaultGatewayName(),
		Namespace: GetGatewayNamespace(),
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
// When the type is unset it mirrors the platform-aware default applied by handleCertificates:
// SelfSigned on XKS (vanilla Kubernetes) and OpenshiftDefaultIngress on OpenShift.
func getCertificateType(gatewayConfig *serviceApi.GatewayConfig) string {
	return string(effectiveCertificateType(gatewayConfig, cluster.GetClusterInfo().Type))
}

func effectiveCertificateType(gatewayConfig *serviceApi.GatewayConfig, clusterType string) infrav1.CertType {
	if gatewayConfig != nil && gatewayConfig.Spec.Certificate != nil && gatewayConfig.Spec.Certificate.Type != "" {
		return gatewayConfig.Spec.Certificate.Type
	}
	return cluster.DefaultGatewayCertificateType(clusterType)
}

func gatewayCertificateSecretName(gatewayConfig *serviceApi.GatewayConfig) string {
	if gatewayConfig.Spec.Certificate != nil && gatewayConfig.Spec.Certificate.SecretName != "" {
		return gatewayConfig.Spec.Certificate.SecretName
	}
	return fmt.Sprintf("%s-tls", gatewayConfig.Name)
}

func handleCertificates(ctx context.Context, rr *odhtypes.ReconciliationRequest, gatewayConfig *serviceApi.GatewayConfig, domain string) (string, error) {
	var certConfig infrav1.CertificateSpec
	if gatewayConfig.Spec.Certificate != nil {
		certConfig = *gatewayConfig.Spec.Certificate
	}
	certConfig.Type = effectiveCertificateType(gatewayConfig, cluster.GetClusterInfo().Type)

	secretName := gatewayCertificateSecretName(gatewayConfig)

	switch certConfig.Type {
	case infrav1.OpenshiftDefaultIngress:
		if err := cluster.PropagateDefaultIngressCertificate(ctx, rr.Client, secretName, GetGatewayNamespace(),
			cluster.WithLabels( // add label easy to know it is from us.
				labels.PlatformPartOf, ServiceName,
			),
			cluster.OwnedBy(gatewayConfig, rr.Client.Scheme()), // set ownerreference for cleanup
		); err != nil {
			return "", fmt.Errorf("failed to propagate default ingress certificate: %w", err)
		}
		return secretName, nil
	case infrav1.SelfSigned:
		// domain parameter already contains the full FQDN (subdomain.baseDomain) from GetFQDN.
		// On XKS, cert-manager is a required platform dependency and owns issuance
		// and renewal. Preserve operator-generated self-signed certificates on OpenShift.
		if cluster.GetClusterInfo().Type == cluster.ClusterTypeKubernetes {
			issuerName, issuerKind := resolveIssuerRef(gatewayConfig.Spec.Certificate)
			cert, err := buildCertManagerCertificate(secretName, GetGatewayNamespace(), secretName, []string{domain}, issuerName, issuerKind)
			if err != nil {
				return "", err
			}
			if err := rr.AddResources(cert); err != nil {
				return "", fmt.Errorf("failed to add gateway Certificate: %w", err)
			}
			logf.FromContext(ctx).V(1).Info("Created cert-manager Certificate for gateway",
				"secret", secretName,
				"issuerName", issuerName,
				"issuerKind", issuerKind,
			)
			return secretName, nil
		}

		if err := cluster.CreateSelfSignedCertificate(ctx, rr.Client, secretName, domain, GetGatewayNamespace(),
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
			ControllerName: GetGatewayControllerName(),
		},
	}

	return rr.AddResources(gatewayClass)
}

func gatewayHTTPSListener(name gwapiv1.SectionName, secretName string, allowedRoutes *gwapiv1.AllowedRoutes) gwapiv1.Listener {
	httpsMode := gwapiv1.TLSModeTerminate
	return gwapiv1.Listener{
		Name: name, Protocol: gwapiv1.HTTPSProtocolType, Port: StandardHTTPSPort,
		TLS: &gwapiv1.GatewayTLSConfig{
			Mode:            &httpsMode,
			CertificateRefs: []gwapiv1.SecretObjectReference{{Name: gwapiv1.ObjectName(secretName)}},
		},
		AllowedRoutes: allowedRoutes,
	}
}

func managedGateway(name string, listeners []gwapiv1.Listener) *gwapiv1.Gateway {
	return &gwapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: GetGatewayNamespace(),
			Labels: map[string]string{IstioRevisionLabel: GetIstioRevisionValue()},
		},
		Spec: gwapiv1.GatewaySpec{GatewayClassName: GatewayClassName, Listeners: listeners},
	}
}

func createGateway(
	rr *odhtypes.ReconciliationRequest,
	certSecretName string,
	domain string,
	legacyDomain string,
	gatewayConfig *serviceApi.GatewayConfig,
) error {
	ingressMode := gatewayConfig.Spec.IngressMode

	listeners := []gwapiv1.Listener{}
	allowedNamespaces := gwapiv1.NamespacesFromSelector
	namespaceSelector := &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      "kubernetes.io/metadata.name",
				Operator: metav1.LabelSelectorOpIn,
				Values: []string{
					GetGatewayNamespace(),
					cluster.GetApplicationNamespace(),
				},
			},
		},
	}
	allowedRoutes := &gwapiv1.AllowedRoutes{
		Namespaces: &gwapiv1.RouteNamespaces{
			From:     &allowedNamespaces,
			Selector: namespaceSelector,
		},
	}

	if certSecretName != "" {
		httpsListener := gatewayHTTPSListener(DefaultGatewayListenerName, certSecretName, allowedRoutes)

		if ingressMode != serviceApi.IngressModeOcpRoute {
			hostname := gwapiv1.Hostname(domain)
			httpsListener.Hostname = &hostname
		}

		listeners = append(listeners, httpsListener)

		// Add legacy listener for LoadBalancer mode to accept requests for legacy hostname
		// (EnvoyFilter will redirect these to the new hostname)
		if ingressMode != serviceApi.IngressModeOcpRoute && legacyDomain != "" {
			legacyHostname := gwapiv1.Hostname(legacyDomain)
			legacyListener := gatewayHTTPSListener(LegacyGatewayListenerName, certSecretName, allowedRoutes)
			legacyListener.Hostname = &legacyHostname
			listeners = append(listeners, legacyListener)
		}
	}

	gateway := managedGateway(GetDefaultGatewayName(), listeners)

	if ingressMode == serviceApi.IngressModeOcpRoute {
		if err := configureClusterIPInfrastructure(rr, gateway, GatewayInfraConfigMapName, GatewayServiceTLSSecretName); err != nil {
			return err
		}
	}

	if err := rr.AddResources(gateway); err != nil {
		return err
	}

	return createAdditionalGateways(rr, gatewayConfig, allowedRoutes)
}

func createAdditionalGateways(
	rr *odhtypes.ReconciliationRequest,
	gatewayConfig *serviceApi.GatewayConfig,
	allowedRoutes *gwapiv1.AllowedRoutes,
) error {
	if len(gatewayConfig.Spec.AdditionalIngresses) == 0 {
		return nil
	}

	for _, ingress := range gatewayConfig.Spec.AdditionalIngresses {
		if additionalIngressHasGatewayConflict(gatewayConfig, ingress.Name) {
			continue
		}
		secretName := additionalGatewayServiceTLSSecretName(ingress.Name)
		gateway := managedGateway(ingress.Name, []gwapiv1.Listener{
			gatewayHTTPSListener(DefaultGatewayListenerName, secretName, allowedRoutes),
		})

		configMapName := additionalGatewayInfrastructureConfigMapName(ingress.Name)
		if err := configureClusterIPInfrastructure(
			rr, gateway,
			configMapName,
			secretName,
		); err != nil {
			return err
		}
		if err := rr.AddResources(gateway); err != nil {
			return fmt.Errorf("failed to add Gateway for additional ingress %q: %w", ingress.Name, err)
		}
	}

	return nil
}

// configureClusterIPInfrastructure creates a ConfigMap for ClusterIP service configuration
// and sets the Gateway's infrastructure reference.
func configureClusterIPInfrastructure(
	rr *odhtypes.ReconciliationRequest,
	gateway *gwapiv1.Gateway,
	configMapName string,
	serviceTLSSecretName string,
) error {
	serviceConfig := fmt.Sprintf(`metadata:
  annotations:
    service.beta.openshift.io/serving-cert-secret-name: "%s"
spec:
  type: ClusterIP
`, serviceTLSSecretName)

	infraConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: GetGatewayNamespace(),
			Labels: map[string]string{
				labels.PlatformPartOf: PartOfGatewayConfig,
			},
		},
		Data: map[string]string{
			"service": serviceConfig,
		},
	}
	if err := rr.AddResources(infraConfigMap); err != nil {
		return err
	}

	gateway.Spec.Infrastructure = &gwapiv1.GatewayInfrastructure{
		ParametersRef: &gwapiv1.LocalParametersReference{
			Group: "",
			Kind:  "ConfigMap",
			Name:  configMapName,
		},
	}

	return nil
}

// createOAuthClient creates an OpenShift OAuth client for integrated authentication.
func createOAuthClient(
	rr *odhtypes.ReconciliationRequest,
	hostname string,
	target authProxyTarget,
	authSecret *corev1.Secret,
) error {
	clientSecretBytes, exists := authSecret.Data["OAUTH2_PROXY_CLIENT_SECRET"]
	if !exists {
		return fmt.Errorf("OAUTH2_PROXY_CLIENT_SECRET not found in secret %s/%s", GetGatewayNamespace(), target.SecretName)
	}
	clientSecret := string(clientSecretBytes)

	redirectURL := fmt.Sprintf("https://%s%s", hostname, OAuthCallbackPath)
	grantMethod := oauthv1.GrantHandlerAuto
	if !target.isDefault() {
		grantMethod = oauthv1.GrantHandlerPrompt
	}
	oauthClient := &oauthv1.OAuthClient{
		ObjectMeta: metav1.ObjectMeta{
			Name: target.ClientID,
		},
		GrantMethod:  grantMethod,
		RedirectURIs: []string{redirectURL},
		Secret:       clientSecret, // encoded string
	}

	return rr.AddResources(oauthClient)
}

// createSecret dynamically creates the kube-auth-proxy-creds secret immediately on the cluster.
func createSecret(ctx context.Context, rr *odhtypes.ReconciliationRequest, clientID, clientSecret, cookieSecret string, target authProxyTarget) (*corev1.Secret, error) {
	gatewayConfig, err := validateGatewayConfig(rr)
	if err != nil {
		return nil, err
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      target.SecretName,
			Namespace: GetGatewayNamespace(),
		},
		Type: corev1.SecretTypeOpaque,
	}

	labelList := map[string]string{
		labels.PlatformPartOf: PartOfGatewayConfig,
	}

	_, err = controllerutil.CreateOrUpdate(ctx, rr.Client, secret, func() error {
		secret.Data = map[string][]byte{
			EnvClientID:     []byte(clientID),
			EnvClientSecret: []byte(clientSecret),
			EnvCookieSecret: []byte(cookieSecret),
		}
		resources.SetLabels(secret, labelList)
		return controllerutil.SetControllerReference(gatewayConfig, secret, rr.Client.Scheme())
	})

	if err != nil {
		return nil, err
	}
	return secret, nil
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

// rejectUnsupportedKubernetesGatewaySpec sets Ready=False when GatewayConfig uses
// OpenShift-only certificate.type or ingressMode on a Kubernetes cluster.
// Returns true when reconciliation should stop (permanent user configuration error).
func rejectUnsupportedKubernetesGatewaySpec(rr *odhtypes.ReconciliationRequest, gatewayConfig *serviceApi.GatewayConfig) bool {
	if cluster.GetClusterInfo().Type != cluster.ClusterTypeKubernetes {
		return false
	}
	msgs := gatewayconfig.KubernetesValidationErrors(gatewayConfig)
	if len(msgs) == 0 {
		return false
	}
	rr.Conditions.MarkFalse(
		ReadyConditionType,
		conditions.WithReason(status.NotReadyReason),
		conditions.WithMessagef("%s", strings.Join(msgs, "; ")),
	)
	return true
}

func getGatewayAuthProxyMaxReplicas(gatewayConfig *serviceApi.GatewayConfig) int32 {
	if gatewayConfig.Spec.AuthProxyMaxReplicas != nil {
		return *gatewayConfig.Spec.AuthProxyMaxReplicas
	}
	return 10
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

// LegacyRedirectInfo contains computed values for legacy hostname redirect functionality.
type LegacyRedirectInfo struct {
	LegacySubdomain string // The legacy subdomain to redirect from (empty if redirect not needed)
	LegacyHostname  string // Full legacy hostname (empty if redirect not needed)
}

// getCurrentSubdomain extracts the current subdomain from GatewayConfig or returns the default.
func getCurrentSubdomain(gc *serviceApi.GatewayConfig) string {
	if gc != nil {
		if subdomain := strings.TrimSpace(gc.Spec.Subdomain); subdomain != "" {
			return subdomain
		}
	}
	return DefaultGatewaySubdomain
}

// computeLegacyRedirectInfo computes the subdomain and legacy hostname information
// for redirect functionality. Returns empty legacy fields if current subdomain equals legacy.
func computeLegacyRedirectInfo(gatewayConfig *serviceApi.GatewayConfig, hostname string) LegacyRedirectInfo {
	currentSubdomain := getCurrentSubdomain(gatewayConfig)

	info := LegacyRedirectInfo{}

	// Only enable legacy redirect if current subdomain differs from legacy
	if currentSubdomain != LegacyGatewaySubdomain {
		info.LegacySubdomain = LegacyGatewaySubdomain
		info.LegacyHostname = strings.Replace(hostname, currentSubdomain+".", LegacyGatewaySubdomain+".", 1)
	}

	return info
}

// calculateAuthConfigHash generates a hash of the authentication secret values
// to detect changes that should trigger a kube-auth-proxy pod restart.
func calculateAuthConfigHash(authSecret *corev1.Secret) string {
	clientID := string(authSecret.Data["OAUTH2_PROXY_CLIENT_ID"])
	clientSecret := string(authSecret.Data["OAUTH2_PROXY_CLIENT_SECRET"])
	cookieSecret := string(authSecret.Data["OAUTH2_PROXY_COOKIE_SECRET"])

	// Calculate SHA256 hash
	hash := sha256.Sum256([]byte(clientID + "\x00" + clientSecret + "\x00" + cookieSecret))
	return hex.EncodeToString(hash[:])
}

// extendAuthConfigHash folds the provider CA bundle into the auth config hash so
// that rotating the CA in the spec.providerCASecretName secret rolls the
// kube-auth-proxy pods. The base hash is kept verbatim when no provider CA is
// configured, so upgrades do not trigger a spurious rollout.
func extendAuthConfigHash(authConfigHash string, providerCA []byte) string {
	sum := sha256.Sum256([]byte(authConfigHash + "\x00" + string(providerCA)))
	return hex.EncodeToString(sum[:])
}

// CalculateRedirectConfigHash hashes the hostname and embedded redirect configuration.
// The pod annotation changes when an upgrade updates the ConfigMap template, so Nginx
// reloads its subPath mounts even if the gateway hostname stays the same.
func CalculateRedirectConfigHash(hostname string) (string, error) {
	config, err := gatewayResources.ReadFile(dashboardRedirectConfigMapTemplate)
	if err != nil {
		return "", fmt.Errorf("read dashboard redirect configuration for rollout hash: %w", err)
	}
	return calculateRedirectConfigHash(hostname, config), nil
}

func calculateRedirectConfigHash(hostname string, config []byte) string {
	h := sha256.Sum256([]byte(hostname + "\x00" + string(config)))
	return hex.EncodeToString(h[:])
}

// getKubeAuthProxyImage returns the kube-auth-proxy image from environment variable.
// For RHOAI deployments, this comes from the CSV (via RHOAI-Build-Config/bundle/additional-images-patch.yaml).
// For ODH deployments, this comes from config/manager/manager.yaml.
// Falls back to a default image for local development/testing only.
func getKubeAuthProxyImage() string {
	if image := os.Getenv("RELATED_IMAGE_ODH_KUBE_AUTH_PROXY_IMAGE"); image != "" {
		return image
	}
	// Fallback for ODH development - pinned to sha256 digest for disconnected/air-gapped support.
	// This build must support the TLS startup flags the gateway emits (--tls-min-version,
	// --tls-cipher-suite and --tls-curve-preferences); it matches the ODH e2e image in
	// manifests-config.yaml so the fallback stays compatible with the rendered proxy args.
	// The pinned build includes the provider CA client-cache fix (kube-auth-proxy#119).
	return "quay.io/opendatahub/odh-kube-auth-proxy@sha256:6d4babb7514c9d7701a193a752d273a8056498761342221997801a36ca157975" // latest as of 2026-10-20
}

// getDashboardRedirectImage returns the nginx image for dashboard redirects.
// For RHOAI deployments, this comes from the CSV (via RHOAI-Build-Config/bundle/additional-images-patch.yaml).
// For ODH deployments and local development/testing, falls back to a publicly accessible UBI9 nginx S2I image.
func getDashboardRedirectImage() string {
	if image := os.Getenv("RELATED_IMAGE_NGINX_126_IMAGE"); image != "" {
		return image
	}
	// Fallback for ODH and local development - publicly accessible UBI9 nginx S2I image
	// This image is identical to registry.redhat.io/ubi9/nginx-126 but does not require authentication
	// Pinned to sha256 digest for disconnected/air-gapped support
	return "registry.access.redhat.com/ubi9/nginx-126@sha256:f0a79ccf21b8780a7534d78ee0e49d1852654f5f0f52e17ebdbe185a71d93253" // 1-1782419572
}

// GetDashboardRouteName returns the platform-specific dashboard route name.
func GetDashboardRouteName() string {
	release := cluster.GetRelease()

	switch release.Name {
	case cluster.OpenDataHub:
		return DashboardRouteNameODH
	case cluster.SelfManagedRhoai, cluster.ManagedRhoai:
		return DashboardRouteNameRHOAI
	default:
		return DashboardRouteNameODH // Fallback to ODH
	}
}

func getAuthProxySecretValues(
	ctx context.Context,
	rr *odhtypes.ReconciliationRequest,
	authMode cluster.AuthenticationMode,
	oidcConfig *serviceApi.OIDCConfig, target authProxyTarget) (string, string, string, error) {
	// Check if kube-auth-proxy-creds already exists and is valid
	existingSecret := &corev1.Secret{}
	secretErr := rr.Client.Get(ctx, types.NamespacedName{
		Name:      target.SecretName,
		Namespace: GetGatewayNamespace(),
	}, existingSecret)

	// Fast exit on NotFound errors
	if secretErr != nil && !k8serr.IsNotFound(secretErr) {
		return "", "", "", fmt.Errorf("failed to check existing secret %s/%s: %w", GetGatewayNamespace(), target.SecretName, secretErr)
	}

	// If secret exists, validate and reuse its values
	if secretErr == nil {
		clientSecretBytes, hasClientSecret := existingSecret.Data["OAUTH2_PROXY_CLIENT_SECRET"]
		cookieSecretBytes, hasCookieSecret := existingSecret.Data["OAUTH2_PROXY_COOKIE_SECRET"]

		if hasClientSecret && hasCookieSecret {
			switch authMode {
			case cluster.AuthModeOIDC:
				// Read the administrator-managed credential below on every reconciliation.
				// Only the cookie key is reused, so client rotation preserves sessions.
			case cluster.AuthModeIntegratedOAuth:
				return target.ClientID, string(clientSecretBytes), string(cookieSecretBytes), nil
			default:
				return "", "", "", fmt.Errorf("auth mode: %s is not supported", authMode)
			}
		}
	}

	var clientSecretValue, clientID string

	switch authMode {
	case cluster.AuthModeOIDC:
		// OIDC mode: get client secret from external secret
		// With additional ingresses, reconciliation continues past the top-level
		// OIDC check so each target can report its own configuration failure.
		if oidcConfig == nil {
			return "", "", "", fmt.Errorf("%w: OIDC configuration is required for gateway %q", errInvalidAuthenticationConfig, target.GatewayName)
		}
		clientID = oidcConfig.ClientID

		// Determine which namespace to use for the secret
		secretNamespace := oidcConfig.SecretNamespace
		if secretNamespace == "" {
			secretNamespace = GetGatewayNamespace() // Default to gateway namespace if not specified
		}

		externalSecret := &corev1.Secret{}
		if err := rr.Client.Get(ctx, types.NamespacedName{
			Name:      oidcConfig.ClientSecretRef.Name,
			Namespace: secretNamespace,
		}, externalSecret); err != nil {
			if k8serr.IsNotFound(err) {
				err = fmt.Errorf("%w: %w", errInvalidAuthenticationConfig, err)
			}
			return "", "", "", fmt.Errorf("failed to get OIDC client secret %s/%s: %w",
				secretNamespace, oidcConfig.ClientSecretRef.Name, err)
		}

		key := oidcConfig.ClientSecretRef.Key
		if key == "" {
			key = "clientSecret"
		}

		if secretValue, exists := externalSecret.Data[key]; exists && len(secretValue) != 0 {
			clientSecretValue = string(secretValue)
		} else {
			return "", "", "", fmt.Errorf("%w: key '%s' is missing or empty in OIDC secret %s/%s",
				errInvalidAuthenticationConfig, key, secretNamespace, oidcConfig.ClientSecretRef.Name)
		}

	case cluster.AuthModeIntegratedOAuth:
		// OAuth mode: generate new client secret
		clientID = target.ClientID

		clientSecretGen, err := cluster.NewSecret("client-secret", "random", 24)
		if err != nil {
			return "", "", "", fmt.Errorf("failed to generate client secret: %w", err)
		}
		clientSecretValue = clientSecretGen.Value

	default:
		return "", "", "", fmt.Errorf("auth mode: %s is not supported", authMode)
	}

	// Preserve the session key independently of the client credential.
	if secretErr == nil && len(existingSecret.Data[EnvCookieSecret]) > 0 {
		return clientID, clientSecretValue, string(existingSecret.Data[EnvCookieSecret]), nil
	}

	// Generate the cookie key only when it does not exist.
	cookieSecretGen, err := cluster.NewSecret("cookie-secret", "random", 32)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to generate cookie secret: %w", err)
	}

	return clientID, clientSecretValue, cookieSecretGen.Value, nil
}

// IsGatewayReferencedSecret returns true when the given Secret is referenced by the
// GatewayConfig singleton — either as the OIDC client secret (spec.oidc.clientSecretRef)
// or the provider CA secret (spec.providerCASecretName). Used as a watch predicate so
// that creating or updating these Secrets triggers GatewayConfig re-reconciliation.
func IsGatewayReferencedSecret(ctx context.Context, cli client.Client, obj client.Object, gatewayNamespace string) bool {
	gatewayConfig := &serviceApi.GatewayConfig{}
	if err := cli.Get(ctx, client.ObjectKey{
		Name: serviceApi.GatewayConfigName,
	}, gatewayConfig); err != nil {
		return false
	}

	secretName := obj.GetName()
	secretNamespace := obj.GetNamespace()

	for _, ingress := range gatewayConfig.Spec.AdditionalIngresses {
		if ingress.Auth.OIDC != nil && secretName == ingress.Auth.OIDC.ClientSecretRef.Name && secretNamespace == authSecretNamespace(ingress.Auth.OIDC.SecretNamespace) {
			return true
		}
	}

	if gatewayConfig.Spec.OIDC != nil {
		oidcSecretNamespace := gatewayConfig.Spec.OIDC.SecretNamespace
		if oidcSecretNamespace == "" {
			oidcSecretNamespace = gatewayNamespace
		}
		if secretName == gatewayConfig.Spec.OIDC.ClientSecretRef.Name && secretNamespace == oidcSecretNamespace {
			return true
		}
	}

	if gatewayConfig.Spec.ProviderCASecretName != "" {
		if secretName == gatewayConfig.Spec.ProviderCASecretName && secretNamespace == gatewayNamespace {
			return true
		}
	}

	return false
}

// IsXKSCertManagerSecret matches the TLS Secrets issued for the XKS gateway and auth proxy.
func IsXKSCertManagerSecret(ctx context.Context, cli client.Client, obj client.Object, gatewayNamespace string) bool {
	if cluster.GetClusterInfo().Type != cluster.ClusterTypeKubernetes || obj.GetNamespace() != gatewayNamespace {
		return false
	}

	gatewayConfig := &serviceApi.GatewayConfig{}
	if err := cli.Get(ctx, client.ObjectKey{Name: serviceApi.GatewayConfigName}, gatewayConfig); err != nil {
		return false
	}
	if gatewayConfig.Spec.OIDC != nil && obj.GetName() == KubeAuthProxyTLSName {
		return true
	}
	return (gatewayConfig.Spec.Certificate == nil || gatewayConfig.Spec.Certificate.Type == "" ||
		gatewayConfig.Spec.Certificate.Type == infrav1.SelfSigned) &&
		obj.GetName() == gatewayCertificateSecretName(gatewayConfig)
}

func isGatewayCertificateOrReferencedSecret(
	ctx context.Context,
	cli client.Client,
	obj client.Object,
	gatewayNamespace string,
) bool {
	return cluster.IsGatewayCertificateSecret(ctx, cli, obj, gatewayNamespace) ||
		IsGatewayReferencedSecret(ctx, cli, obj, gatewayNamespace)
}

// detectAndSetIngressMode detects the ingress mode from an existing Gateway Service and updates
// the GatewayConfig to match. This preserves existing Gateway configuration when ingressMode is unset.
func detectAndSetIngressMode(ctx context.Context, rr *odhtypes.ReconciliationRequest, gatewayConfig *serviceApi.GatewayConfig) error {
	l := logf.FromContext(ctx).WithName("detectAndSetIngressMode")

	svc := &corev1.Service{}
	err := rr.Client.Get(ctx, client.ObjectKey{
		Name:      GetDefaultGatewayServiceFullName(),
		Namespace: GetGatewayNamespace(),
	}, svc)

	if k8serr.IsNotFound(err) {
		// On XKS, default to LoadBalancer (no OpenShift Routes).
		// On OpenShift, default to OcpRoute.
		if cluster.GetClusterInfo().Type == cluster.ClusterTypeKubernetes {
			gatewayConfig.Spec.IngressMode = serviceApi.IngressModeLoadBalancer
		} else {
			gatewayConfig.Spec.IngressMode = serviceApi.IngressModeOcpRoute
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get Gateway Service: %w", err)
	}

	var detectedMode serviceApi.IngressMode
	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
		detectedMode = serviceApi.IngressModeLoadBalancer
	} else {
		detectedMode = serviceApi.IngressModeOcpRoute
	}

	l.Info("Detected ingress mode from existing Gateway Service", "mode", detectedMode)

	gatewayConfig.Spec.IngressMode = detectedMode

	if err := rr.Client.Update(ctx, gatewayConfig); err != nil {
		return fmt.Errorf("failed to update GatewayConfig with detected mode: %w", err)
	}

	return nil
}

// reconcileGatewayForModeChange deletes the Gateway if its configuration doesn't match
// the desired ingress mode. SSA won't remove fields like hostname, so we force recreation.
func reconcileGatewayForModeChange(ctx context.Context, rr *odhtypes.ReconciliationRequest, desiredMode serviceApi.IngressMode) error {
	l := logf.FromContext(ctx).WithName("reconcileGatewayForModeChange")

	gateway := &gwapiv1.Gateway{}
	err := rr.Client.Get(ctx, client.ObjectKey{
		Name:      GetDefaultGatewayName(),
		Namespace: GetGatewayNamespace(),
	}, gateway)

	if k8serr.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get Gateway: %w", err)
	}

	// OcpRoute: no hostname, has infrastructure
	// LoadBalancer: has hostname, no infrastructure
	var hasHostname bool
	for _, listener := range gateway.Spec.Listeners {
		if listener.Name == DefaultGatewayListenerName && listener.Hostname != nil {
			hasHostname = true
			break
		}
	}
	hasInfrastructure := gateway.Spec.Infrastructure != nil

	wantsHostname := desiredMode == serviceApi.IngressModeLoadBalancer
	wantsInfrastructure := desiredMode == serviceApi.IngressModeOcpRoute

	if hasHostname == wantsHostname && hasInfrastructure == wantsInfrastructure {
		return nil
	}

	l.Info("Deleting Gateway for ingress mode change", "desiredMode", desiredMode)
	if err := rr.Client.Delete(ctx, gateway); err != nil {
		return fmt.Errorf("failed to delete Gateway: %w", err)
	}

	// Return error to requeue and let the Gateway fully terminate before recreating
	return errors.New("gateway deleted for mode change, requeuing to recreate")
}
