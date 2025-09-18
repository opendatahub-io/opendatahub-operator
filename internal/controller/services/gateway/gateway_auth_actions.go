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

const (
	AuthClientID = "odh"
)

func createKubeAuthProxyInfrastructure(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createAuthProxy")

	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return errors.New("instance is not of type *services.GatewayConfig")
	}

	l.V(1).Info("creating auth proxy for gateway", "gateway", gatewayConfig.Name)

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

	condition := common.Condition{
		Type:   status.ConditionTypeReady,
		Status: metav1.ConditionFalse,
		Reason: status.NotReadyReason,
	}

	// generate client secret once for both kube-auth-proxy and OAuth client
	var clientSecret string
	if authMode == AuthModeIntegratedOAuth {
		clientSecretGen, err := secretgenerator.NewSecret("client-secret", "random", 24)
		if err != nil {
			condition.Message = fmt.Sprintf("Failed to generate client secret: %v", err)
			gatewayConfig.SetConditions([]common.Condition{condition})
			return err
		}
		clientSecret = clientSecretGen.Value
	}

	if err := deployKubeAuthProxy(ctx, rr, oidcConfig, clientSecret); err != nil {
		condition.Message = fmt.Sprintf("Failed to deploy auth proxy: %v", err)
		gatewayConfig.SetConditions([]common.Condition{condition})
		return err
	}

	if authMode == AuthModeIntegratedOAuth {
		err = createOAuthClient(ctx, rr, clientSecret)
		if err != nil {
			condition.Message = fmt.Sprintf("Failed to create OAuth client: %v", err)
			gatewayConfig.SetConditions([]common.Condition{condition})
			return err
		}
	}

	err = createOAuthCallbackRoute(rr)
	if err != nil {
		condition.Message = fmt.Sprintf("Failed to create OAuth callback route: %v", err)
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

func deployKubeAuthProxy(ctx context.Context, rr *odhtypes.ReconciliationRequest, oidcConfig *serviceApi.OIDCConfig, clientSecret string) error {
	l := logf.FromContext(ctx).WithName("deployAuthProxy")

	if oidcConfig != nil {
		l.V(1).Info("configuring kube-auth-proxy for external OIDC",
			"issuerURL", oidcConfig.IssuerURL,
			"clientID", oidcConfig.ClientID,
			"secretRef", oidcConfig.ClientSecretRef.Name)
	} else {
		l.V(1).Info("configuring kube-auth-proxy for OpenShift OAuth")
	}

	err := createKubeAuthProxySecret(ctx, rr, clientSecret, oidcConfig)
	if err != nil {
		return err
	}

	secret := &corev1.Secret{}
	err = rr.Client.Get(ctx, types.NamespacedName{
		Name:      "kube-auth-proxy-creds",
		Namespace: gatewayNamespace,
	}, secret)
	if err != nil {
		if k8serr.IsNotFound(err) {
			// secret not ready yet - trying next reconciliation
			l.V(1).Info("kube-auth-proxy secret not found, creating secret", "secret", "kube-auth-proxy-creds")
			return nil
		}
		// log but continue - secret might still be getting created
		l.V(1).Info("unable to verify kube-auth-proxy secret status, creating secret", "error", err, "secret", "kube-auth-proxy-creds")
		return nil
	}

	l.V(1).Info("secret is ready, proceeding with dependent resources", "secret", "kube-auth-proxy-creds")

	err = createKubeAuthProxyService(rr)
	if err != nil {
		return err
	}

	err = createKubeAuthProxyDeployment(ctx, rr, oidcConfig)
	if err != nil {
		return err
	}

	return nil
}

func createKubeAuthProxySecret(ctx context.Context, rr *odhtypes.ReconciliationRequest, clientSecret string, oidcConfig *serviceApi.OIDCConfig) error {
	cookieSecretGen, err := secretgenerator.NewSecret("cookie-secret", "random", 32)
	if err != nil {
		return fmt.Errorf("failed to generate cookie secret: %w", err)
	}

	clientId := AuthClientID
	clientSecretValue := clientSecret

	if oidcConfig != nil {
		clientId = oidcConfig.ClientID

		secret := &corev1.Secret{}
		err := rr.Client.Get(ctx, types.NamespacedName{
			Name:      oidcConfig.ClientSecretRef.Name,
			Namespace: gatewayNamespace,
		}, secret)
		if err != nil {
			return fmt.Errorf("failed to get OIDC client secret %s/%s: %w",
				gatewayNamespace, oidcConfig.ClientSecretRef.Name, err)
		}

		key := oidcConfig.ClientSecretRef.Key
		if key == "" {
			key = "clientSecret"
		}
		if secretValue, exists := secret.Data[key]; exists {
			clientSecretValue = string(secretValue)
		} else {
			return fmt.Errorf("key '%s' not found in secret %s/%s",
				key, gatewayNamespace, oidcConfig.ClientSecretRef.Name)
		}
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kube-auth-proxy-creds",
			Namespace: gatewayNamespace,
			Labels: map[string]string{
				"app": "kube-auth-proxy",
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"OAUTH2_PROXY_CLIENT_ID":     clientId,
			"OAUTH2_PROXY_CLIENT_SECRET": clientSecretValue,
			"OAUTH2_PROXY_COOKIE_SECRET": cookieSecretGen.Value,
		},
	}

	return rr.AddResources(secret)
}

func createKubeAuthProxyDeployment(ctx context.Context, rr *odhtypes.ReconciliationRequest, oidcConfig *serviceApi.OIDCConfig) error {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kube-auth-proxy",
			Namespace: gatewayNamespace,
			Labels: map[string]string{
				"app": "kube-auth-proxy",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "kube-auth-proxy",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "kube-auth-proxy",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "kube-auth-proxy",
							// TODO: replace with conflux kube auth proxy image
							Image: "quay.io/jtanner/kube-auth-proxy@sha256:434580fd42d73727d62566ff6d8336219a31b322798b48096ed167daaec42f07",
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 4180,
									Name:          "http",
								},
								{
									ContainerPort: 8443,
									Name:          "https",
								},
							},
							Args: buildOAuth2ProxyArgs(ctx, rr, oidcConfig),
							Env: []corev1.EnvVar{
								{
									Name: "OAUTH2_PROXY_CLIENT_ID",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "kube-auth-proxy-creds",
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
												Name: "kube-auth-proxy-creds",
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
												Name: "kube-auth-proxy-creds",
											},
											Key: "OAUTH2_PROXY_COOKIE_SECRET",
										},
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "tls-certs",
									MountPath: "/etc/tls/private",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "tls-certs",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "kube-auth-proxy-tls",
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

func buildOAuth2ProxyArgs(ctx context.Context, rr *odhtypes.ReconciliationRequest, oidcConfig *serviceApi.OIDCConfig) []string {
	clusterDomain, err := cluster.GetDomain(ctx, rr.Client)
	if err != nil {
		clusterDomain = "cluster.local" // I guess?
	}

	redirectURL := fmt.Sprintf("https://%s.%s/oauth2/callback", gatewayName, clusterDomain)
	baseArgs := []string{
		"--http-address=0.0.0.0:4180",
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
			"--tls-cert-file=/etc/tls/private/tls.crt",
			"--tls-key-file=/etc/tls/private/tls.key",
			"--https-address=0.0.0.0:8443",
		}...)
	}
}

func createKubeAuthProxyService(rr *odhtypes.ReconciliationRequest) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kube-auth-proxy",
			Namespace: gatewayNamespace,
			Labels: map[string]string{
				"app": "kube-auth-proxy",
			},
			Annotations: map[string]string{
				"service.beta.openshift.io/serving-cert-secret-name": "kube-auth-proxy-tls",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": "kube-auth-proxy",
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "https",
					Port:       8443,
					TargetPort: intstr.FromInt(8443),
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

	redirectURL := fmt.Sprintf("https://%s.%s/oauth2/callback", gatewayName, clusterDomain)

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
	pathPrefix := gwapiv1.PathMatchPathPrefix
	gatewayNS := gwapiv1.Namespace(gatewayNamespace)
	port := gwapiv1.PortNumber(8443)
	path := "/oauth2"

	httpRoute := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "oauth-callback-route",
			Namespace: gatewayNamespace,
		},
		Spec: gwapiv1.HTTPRouteSpec{
			CommonRouteSpec: gwapiv1.CommonRouteSpec{
				ParentRefs: []gwapiv1.ParentReference{
					{
						Name:      gwapiv1.ObjectName(gatewayName),
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
									Name: "kube-auth-proxy",
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
