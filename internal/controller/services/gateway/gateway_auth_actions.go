package gateway

import (
	"context"
	"errors"
	"fmt"

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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

type AuthMode string

const (
	AuthModeIntegratedOAuth AuthMode = "IntegratedOAuth"
	AuthModeOIDC            AuthMode = "OIDC"
	AuthModeNone            AuthMode = "None"
)

func createKubeAuthProxyInfrastructure(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createAuthProxy")

	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return errors.New("instance is not of type *services.GatewayConfig")
	}

	l.V(1).Info("creating auth proxy for gateway", "gateway", gatewayConfig.Name)

	// Resolve domain consistently with createGatewayInfrastructure
	domain, err := ResolveDomain(ctx, rr.Client, gatewayConfig, DefaultGatewayName)
	if err != nil {
		return fmt.Errorf("failed to resolve domain: %w", err)
	}

	authMode, err := detectClusterAuthMode(ctx, rr)
	if err != nil {
		return fmt.Errorf("failed to detect cluster authentication mode: %w", err)
	}

	l.V(1).Info("detected cluster authentication mode", "mode", authMode)

	if errorCondition := validateOIDCConfig(authMode, gatewayConfig.Spec.OIDC); errorCondition != nil {
		gatewayConfig.SetConditions([]common.Condition{*errorCondition})
		return nil
	}

	if condition := checkAuthModeNone(authMode); condition != nil {
		gatewayConfig.SetConditions([]common.Condition{*condition})
		return nil
	}

	var oidcConfig *serviceApi.OIDCConfig
	if authMode == AuthModeOIDC {
		oidcConfig = gatewayConfig.Spec.OIDC
	}

	// get or generate secrets for kube-auth-proxy (handles OAuth and OIDC modes)
	clientSecret, cookieSecret, err := getOrGenerateSecrets(ctx, rr, authMode)
	if err != nil {
		condition := CreateErrorCondition("Failed to get or generate secrets", err)
		gatewayConfig.SetConditions([]common.Condition{condition})
		return err
	}

	if err := deployKubeAuthProxy(ctx, rr, oidcConfig, clientSecret, cookieSecret, domain); err != nil {
		condition := CreateErrorCondition("Failed to deploy auth proxy", err)
		gatewayConfig.SetConditions([]common.Condition{condition})
		return err
	}

	if authMode == AuthModeIntegratedOAuth {
		err = createOAuthClient(ctx, rr, clientSecret)
		if err != nil {
			condition := CreateErrorCondition("Failed to create OAuth client", err)
			gatewayConfig.SetConditions([]common.Condition{condition})
			return err
		}
	}

	err = createOAuthCallbackRoute(rr)
	if err != nil {
		condition := CreateErrorCondition("Failed to create OAuth callback route", err)
		gatewayConfig.SetConditions([]common.Condition{condition})
		return err
	}

	gatewayConfig.SetConditions([]common.Condition{{
		Type:    status.ConditionTypeReady,
		Status:  metav1.ConditionTrue,
		Reason:  status.ReadyReason,
		Message: "Auth proxy deployed successfully",
	}})

	return nil
}

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
	if authMode == AuthModeOIDC && oidcConfig == nil {
		return &common.Condition{
			Type:    status.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  status.NotReadyReason,
			Message: "Cluster is in OIDC mode but GatewayConfig has no OIDC configuration",
		}
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

func getOrGenerateSecrets(ctx context.Context, rr *odhtypes.ReconciliationRequest, authMode AuthMode) (string, string, error) {
	existingSecret := &corev1.Secret{}
	secretErr := rr.Client.Get(ctx, types.NamespacedName{
		Name:      KubeAuthProxySecretsName,
		Namespace: GatewayNamespace,
	}, existingSecret)

	if secretErr == nil {
		clientSecretBytes, hasClientSecret := existingSecret.Data["OAUTH2_PROXY_CLIENT_SECRET"]
		cookieSecretBytes, hasCookieSecret := existingSecret.Data["OAUTH2_PROXY_COOKIE_SECRET"]

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

func deployKubeAuthProxy(ctx context.Context, rr *odhtypes.ReconciliationRequest, oidcConfig *serviceApi.OIDCConfig, clientSecret, cookieSecret string, domain string) error {
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

	err = createKubeAuthProxyDeployment(rr, oidcConfig, domain)
	if err != nil {
		return err
	}

	return nil
}

func createKubeAuthProxySecret(ctx context.Context, rr *odhtypes.ReconciliationRequest, clientSecret, cookieSecret string, oidcConfig *serviceApi.OIDCConfig) error {
	clientId := AuthClientID
	clientSecretValue := clientSecret

	if oidcConfig != nil {
		clientId = oidcConfig.ClientID

		secret := &corev1.Secret{}
		err := rr.Client.Get(ctx, types.NamespacedName{
			Name:      oidcConfig.ClientSecretRef.Name,
			Namespace: GatewayNamespace,
		}, secret)
		if err != nil {
			return fmt.Errorf("failed to get OIDC client secret %s/%s: %w",
				GatewayNamespace, oidcConfig.ClientSecretRef.Name, err)
		}

		key := oidcConfig.ClientSecretRef.Key
		if key == "" {
			key = DefaultClientSecretKey
		}
		if secretValue, exists := secret.Data[key]; exists {
			clientSecretValue = string(secretValue)
		} else {
			return fmt.Errorf("key '%s' not found in secret %s/%s",
				key, GatewayNamespace, oidcConfig.ClientSecretRef.Name)
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
			"OAUTH2_PROXY_CLIENT_ID":     clientId,
			"OAUTH2_PROXY_CLIENT_SECRET": clientSecretValue,
			"OAUTH2_PROXY_COOKIE_SECRET": cookieSecret,
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

func createKubeAuthProxyDeployment(rr *odhtypes.ReconciliationRequest, oidcConfig *serviceApi.OIDCConfig, domain string) error {
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
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: KubeAuthProxyName,
							// TODO: replace with conflux kube auth proxy image
							Image: KubeAuthProxyImage,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: AuthProxyHTTPPort,
									Name:          "http",
								},
								{
									ContainerPort: AuthProxyHTTPSPort,
									Name:          "https",
								},
							},
							Args: buildOAuth2ProxyArgs(oidcConfig, domain),
							Env: []corev1.EnvVar{
								{
									Name: "OAUTH2_PROXY_CLIENT_ID",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: KubeAuthProxySecretsName,
											},
											Key: "OAUTH2_PROXY_CLIENT_ID",
										},
									},
								},
								{
									Name: "OAUTH2_PROXY_CLIENT_SECRET",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: KubeAuthProxySecretsName,
											},
											Key: "OAUTH2_PROXY_CLIENT_SECRET",
										},
									},
								},
								{
									Name: "OAUTH2_PROXY_COOKIE_SECRET",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: KubeAuthProxySecretsName,
											},
											Key: "OAUTH2_PROXY_COOKIE_SECRET",
										},
									},
								},
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

func buildOAuth2ProxyArgs(oidcConfig *serviceApi.OIDCConfig, domain string) []string {
	redirectURL := fmt.Sprintf("https://%s/oauth2/callback", domain)
	baseArgs := []string{
		fmt.Sprintf("--http-address=0.0.0.0:%d", AuthProxyHTTPPort),
		"--email-domain=*",
		"--upstream=static://200",
		"--skip-provider-button",
		"--pass-access-token=true",
		"--set-xauthrequest=true",
		"--redirect-url=" + redirectURL,
	}

	if oidcConfig != nil {
		return append(baseArgs, []string{
			"--provider=oidc",
			"--oidc-issuer-url=" + oidcConfig.IssuerURL,
			"--ssl-insecure-skip-verify=true",
		}...)
	} else {
		return append(baseArgs, []string{
			"--provider=openshift",
			"--scope=user:full",
			"--tls-cert-file=" + TLSCertsMountPath + "/tls.crt",
			"--tls-key-file=" + TLSCertsMountPath + "/tls.key",
			fmt.Sprintf("--https-address=0.0.0.0:%d", AuthProxyHTTPSPort),
		}...)
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

func createOAuthClient(ctx context.Context, rr *odhtypes.ReconciliationRequest, clientSecret string) error {
	clusterDomain, err := cluster.GetDomain(ctx, rr.Client)
	if err != nil {
		return fmt.Errorf("failed to get cluster domain: %w", err)
	}

	// Get platform-specific gateway name
	redirectURL := fmt.Sprintf("https://%s.%s/oauth2/callback", DefaultGatewayName, clusterDomain)

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

func createOAuthCallbackRoute(rr *odhtypes.ReconciliationRequest) error {
	// Get platform-specific gateway name
	pathPrefix := gwapiv1.PathMatchPathPrefix
	gatewayNS := gwapiv1.Namespace(GatewayNamespace)
	port := gwapiv1.PortNumber(AuthProxyHTTPSPort)
	path := AuthProxyOAuth2Path

	httpRoute := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      OAuthCallbackRouteName,
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
			Rules: []gwapiv1.HTTPRouteRule{
				{
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
									Name: gwapiv1.ObjectName(KubeAuthProxyName),
									Port: &port,
								},
							},
						},
					},
				},
			},
		},
	}

	return rr.AddResources(httpRoute)
}
