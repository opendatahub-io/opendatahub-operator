/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gateway

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

//go:embed resources
var gatewayResources embed.FS

// Create GatewayClass, Gateway with certificate.
func createGatewayInfrastructure(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createGatewayInfrastructure")

	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return errors.New("instance is not of type *services.GatewayConfig")
	}
	l.V(1).Info("Creating Gateway infrastructure", "gateway", gatewayConfig.Name)

	if err := createGatewayClass(rr); err != nil {
		return fmt.Errorf("failed to create GatewayClass %s: %w", GatewayClassName, err)
	}

	hostname, err := GetFQDN(ctx, rr.Client, gatewayConfig)
	if err != nil {
		return err
	}

	certSecretName, err := handleCertificates(ctx, rr, gatewayConfig, hostname)
	if err != nil {
		return fmt.Errorf("failed to handle certificates: %w", err)
	}

	if err := createGateway(rr, certSecretName, hostname); err != nil {
		return fmt.Errorf("failed to create Gateway: %w", err)
	}

	l.V(1).Info("Successfully created Gateway infrastructure",
		"gateway", DefaultGatewayName,
		"namespace", GatewayNamespace,
		"domain", hostname,
		"certificateType", getCertificateType(gatewayConfig))

	return nil
}

// Check authentication mode and deploy auth proxy (secret + service + deployment) + OAuth client (if integrated mode) + HTTPRoute + DestinationRule.
func createKubeAuthProxyInfrastructure(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createAuthProxy")

	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return errors.New("instance is not of type *services.GatewayConfig")
	}

	l.V(1).Info("creating auth proxy for gateway", "gateway", gatewayConfig.Name)

	authMode, err := cluster.GetClusterAuthenticationMode(ctx, rr.Client)
	if err != nil {
		return fmt.Errorf("failed to detect cluster authentication mode: %w", err)
	}
	l.V(1).Info("detected cluster authentication mode", "mode", authMode)

	// Reset the condition to false
	rr.Conditions.MarkFalse(ReadyConditionType)

	kubeAuthProxyDeploymentTemplates := odhtypes.TemplateInfo{
		FS:   gatewayResources,
		Path: kubeAuthProxyDeploymentOauthTemplate,
	}

	var oidcConfig *serviceApi.OIDCConfig
	switch authMode {
	case cluster.AuthModeOIDC:
		if gatewayConfig.Spec.OIDC == nil {
			rr.Conditions.MarkFalse(
				ReadyConditionType,
				conditions.WithReason(status.NotReadyReason),
				conditions.WithMessage(status.AuthProxyOIDCModeWithoutConfigMessage),
			)
			return nil // TODO: is this logic correct? no oidc but to use oidc and not error out.
		}
		oidcConfig = gatewayConfig.Spec.OIDC
		l.V(1).Info("configuring "+KubeAuthProxyName+" for external OIDC",
			"issuerURL", oidcConfig.IssuerURL,
			"clientID", oidcConfig.ClientID,
			"secretRef", oidcConfig.ClientSecretRef.Name)
		kubeAuthProxyDeploymentTemplates = odhtypes.TemplateInfo{
			FS:   gatewayResources,
			Path: kubeAuthProxyDeploymentOidcTemplate,
		}
	case cluster.AuthModeIntegratedOAuth: // default mode.
		l.V(1).Info("configuring " + KubeAuthProxyName + " for OpenShift OAuth")

	case cluster.AuthModeNone:
		rr.Conditions.MarkTrue(
			ReadyConditionType,
			conditions.WithReason(status.ReadyReason), // TODO: is this logic correct? user do not want it, we should mark it as ready.
			conditions.WithMessage(status.AuthProxyExternalAuthNoDeploymentMessage),
		)
		return nil
	}

	// Get secret values for both OIDC and IntegratedOAuth modes
	clientID, clientSecret, cookieSecret, err := getSecretValues(ctx, rr, authMode, oidcConfig)
	if err != nil {
		return fmt.Errorf("failed to get secret values: %w", err)
	}

	// Create the secret dynamically first
	if err := createSecret(ctx, rr, clientID, clientSecret, cookieSecret); err != nil {
		rr.Conditions.MarkFalse(
			ReadyConditionType,
			conditions.WithReason(status.NotReadyReason),
			conditions.WithMessage("%s: %v", status.AuthProxyFailedGenerateSecretMessage, err),
		)
		return fmt.Errorf("failed to create auth proxy secret: %w", err)
	}

	// For IntegratedOAuth mode, create OAuth client after secret is created
	if authMode == cluster.AuthModeIntegratedOAuth {
		if err := createOAuthClient(ctx, rr, gatewayConfig); err != nil {
			rr.Conditions.MarkFalse(
				ReadyConditionType,
				conditions.WithReason(status.NotReadyReason),
				conditions.WithMessage("%s: %v", status.AuthProxyFailedOAuthClientMessage, err),
			)
			return fmt.Errorf("failed to create OAuth client: %w", err)
		}
		l.V(1).Info("OAuth client created successfully")
	}
	rr.Templates = append(rr.Templates, kubeAuthProxyDeploymentTemplates)
	// Add other KubeAuthProxy templates to the reconciliation request
	kubeAuthProxyCommonTemplates := []odhtypes.TemplateInfo{
		{
			FS:   gatewayResources,
			Path: kubeAuthProxyServiceTemplate,
		},
		{
			FS:   gatewayResources,
			Path: kubeAuthProxyHTTPRouteTemplate,
		},
		{
			FS:   gatewayResources,
			Path: destinationRuleTemplate,
		},
	}
	rr.Templates = append(rr.Templates, kubeAuthProxyCommonTemplates...)

	return nil
}

func createEnvoyFilter(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createEnvoyFilter")

	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return errors.New("instance is not of type *services.GatewayConfig")
	}

	l.V(1).Info("creating EnvoyFilter for gateway", "gateway", gatewayConfig.Name)

	// Add EnvoyFilter template to the reconciliation request
	envoyFilterTemplate := []odhtypes.TemplateInfo{
		{
			FS:   gatewayResources,
			Path: envoyFilterTemplate,
		},
	}
	rr.Templates = append(rr.Templates, envoyFilterTemplate...)

	return nil
}

func getTemplateData(ctx context.Context, rr *odhtypes.ReconciliationRequest) (map[string]any, error) {
	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return nil, errors.New("instance is not of type *services.GatewayConfig")
	}

	// Get domain for redirect URL, if not set in spec then fall back to cluster domain
	hostname, err := GetFQDN(ctx, rr.Client, gatewayConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve domain: %w", err)
	}

	// Calculate auth config hash for triggering pod restarts on secret changes
	authConfigHash := ""
	authSecret := &corev1.Secret{}
	if err := rr.Client.Get(ctx, types.NamespacedName{
		Name:      KubeAuthProxySecretsName,
		Namespace: GatewayNamespace,
	}, authSecret); err != nil {
		// secret doesn't exist yet, use empty hash
		if !k8serr.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get auth secret for hash calculation: %w", err)
		}
	} else {
		// Secret exists, calculate hash from its value
		authConfigHash = calculateAuthConfigHash(authSecret)
	}

	// Get AuthProxyTimeout in proceed of GatewayConfig, env variable GATEWAY_AUTH_TIMEOUT, then default to 5s
	// zero value (when not set) returns "0s"
	authProxyTimeout := gatewayConfig.Spec.AuthProxyTimeout.Duration.String()
	if authProxyTimeout == "0s" {
		authProxyTimeout = os.Getenv("GATEWAY_AUTH_TIMEOUT")
		if authProxyTimeout == "" {
			authProxyTimeout = "5s"
		}
	}

	templateData := map[string]any{
		"GatewayNamespace":         GatewayNamespace,
		"GatewayName":              DefaultGatewayName,
		"GatewayClassName":         GatewayClassName,
		"GatewayHostname":          hostname,
		"KubeAuthProxyServiceName": KubeAuthProxyName,
		"KubeAuthProxySecretsName": KubeAuthProxySecretsName,
		"KubeAuthProxyTLSName":     KubeAuthProxyTLSName,
		"OAuthCallbackRouteName":   OAuthCallbackRouteName,
		"KubeAuthProxyImage":       GetKubeAuthProxyImage(),
		"AuthProxyHTTPPort":        AuthProxyHTTPPort,
		"AuthProxyMetricsPort":     AuthProxyMetricsPort,
		"StandardHTTPSPort":        StandardHTTPSPort,
		"GatewayHTTPSPort":         GatewayHTTPSPort,
		"AuthProxyOAuth2Path":      AuthProxyOAuth2Path,
		"AuthProxyCookieName":      AuthProxyCookieName,
		"TLSCertsVolumeName":       TLSCertsVolumeName,
		"TLSCertsMountPath":        TLSCertsMountPath,
		"EnvoyFilter":              AuthnFilterName,
		"RedirectURL":              fmt.Sprintf("https://%s/oauth2/callback", hostname),
		"DestinationRuleName":      DestinationRuleName,
		"CookieExpire":             gatewayConfig.Spec.Cookie.Expire.Duration.String(),
		"CookieRefresh":            gatewayConfig.Spec.Cookie.Refresh.Duration.String(),
		"AuthConfigHash":           authConfigHash,
		"AuthProxyTimeout":         authProxyTimeout,
	}

	// Add OIDC-specific fields only if OIDC config is present
	if gatewayConfig.Spec.OIDC != nil {
		templateData["OIDCIssuerURL"] = gatewayConfig.Spec.OIDC.IssuerURL
	}

	return templateData, nil
}

func syncGatewayConfigStatus(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	_, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return errors.New("instance is not of type *services.GatewayConfig")
	}

	gateway := &gwapiv1.Gateway{}
	err := rr.Client.Get(ctx, types.NamespacedName{
		Name:      DefaultGatewayName,
		Namespace: GatewayNamespace,
	}, gateway)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			rr.Conditions.MarkFalse(
				ReadyConditionType,
				conditions.WithReason(status.NotReadyReason),
				conditions.WithMessage(status.GatewayNotFoundMessage),
			)
			return nil
		}
		return fmt.Errorf("failed to get Gateway: %w", err)
	}

	isReady := false
	for _, condition := range gateway.Status.Conditions {
		if condition.Type == string(gwapiv1.GatewayConditionAccepted) && condition.Status == metav1.ConditionTrue {
			isReady = true
			break
		}
	}

	if isReady {
		rr.Conditions.MarkTrue(
			ReadyConditionType,
			conditions.WithObservedGeneration(rr.Instance.GetGeneration()),
			conditions.WithReason(status.ReadyReason),
			conditions.WithMessage(status.GatewayReadyMessage),
		)
		return nil
	}
	rr.Conditions.MarkFalse(
		ReadyConditionType,
		conditions.WithReason(status.NotReadyReason),
		conditions.WithMessage(status.GatewayNotReadyMessage),
	)
	return nil
}
