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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

//go:embed resources
var gatewayResources embed.FS

// cretae gatewayclass, gateway with cert.
func createGatewayInfrastructure(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createGatewayInfrastructure")

	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return errors.New("instance is not of type *services.GatewayConfig")
	}
	l.V(1).Info("Creating Gateway infrastructure", "gateway", gatewayConfig.Name)

	if err := createGatewayClass(rr); err != nil {
		return fmt.Errorf("failed to create GatewayClass: %w", err)
	}

	domain, err := getDomain(ctx, rr, gatewayConfig)
	if err != nil {
		return err
	}

	certSecretName, err := handleCertificates(ctx, rr, gatewayConfig, domain)
	if err != nil {
		return fmt.Errorf("failed to handle certificates: %w", err)
	}

	if err := createGateway(rr, certSecretName, domain); err != nil {
		return fmt.Errorf("failed to create Gateway: %w", err)
	}

	l.V(1).Info("Successfully created Gateway infrastructure",
		"gateway", gatewayName,
		"namespace", gatewayNamespace,
		"domain", domain,
		"certificateType", getCertificateType(gatewayConfig))

	return nil
}

// check mode and deploy auth proxy(secret + svc + deployment) + oauth client (if integrated mode) + httproute.
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

	var oidcConfig *serviceApi.OIDCConfig
	switch authMode {
	case AuthModeOIDC:
		if gatewayConfig.Spec.OIDC == nil {
			rr.Conditions.MarkFalse(
				status.ConditionTypeReady,
				conditions.WithReason(status.NotReadyReason),
				conditions.WithMessage(status.AuthProxyOIDCModeWithoutConfigMessage),
			)
			return nil // TODO: is this logic correct? no oidc but to use oidc and not error out.
		}
		oidcConfig = gatewayConfig.Spec.OIDC
		l.V(1).Info("configuring "+kubeAuthProxyName+" for external OIDC",
			"issuerURL", oidcConfig.IssuerURL,
			"clientID", oidcConfig.ClientID,
			"secretRef", oidcConfig.ClientSecretRef.Name)

	case AuthModeIntegratedOAuth:
		l.V(1).Info("configuring " + kubeAuthProxyName + " for OpenShift OAuth")

	case AuthModeNone:
		rr.Conditions.MarkFalse(
			status.ConditionTypeReady,
			conditions.WithReason(status.NotReadyReason), // TODO: is this logic correct? user do not want it, we should not mark it as not ready.
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
			status.ConditionTypeReady,
			conditions.WithReason(status.NotReadyReason),
			conditions.WithMessage("%s: %v", status.AuthProxyFailedGenerateSecretMessage, err),
		)
		return fmt.Errorf("failed to create auth proxy secret: %w", err)
	}

	// For IntegratedOAuth mode, create OAuth client after secret is created
	if authMode == AuthModeIntegratedOAuth {
		if err := createOAuthClient(ctx, rr); err != nil {
			rr.Conditions.MarkFalse(
				status.ConditionTypeReady,
				conditions.WithReason(status.NotReadyReason),
				conditions.WithMessage("%s: %v", status.AuthProxyFailedOAuthClientMessage, err),
			)
			return fmt.Errorf("failed to create OAuth client: %w", err)
		}
		l.V(1).Info("OAuth client created successfully")
	}

	// Add KubeAuthProxy templates to the reconciliation request
	kubeAuthProxyTemplates := []odhtypes.TemplateInfo{
		{
			FS:   gatewayResources,
			Path: KubeAuthProxyDeploymentTemplate,
		},
		{
			FS:   gatewayResources,
			Path: KubeAuthProxyServiceTemplate,
		},
		{
			FS:   gatewayResources,
			Path: KubeAuthProxyHTTPRouteTemplate,
		},
	}
	rr.Templates = append(rr.Templates, kubeAuthProxyTemplates...)

	return nil
}

func createEnvoyFilter(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createEnvoyFilter")

	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return errors.New("instance is not of type *services.GatewayConfig")
	}

	l.V(1).Info("creating  envoyfilter for gateway", "gateway", gatewayConfig.Name)

	// Add EnvoyFilter template to the reconciliation request
	envoyFilterTemplate := []odhtypes.TemplateInfo{
		{
			FS:   gatewayResources,
			Path: EnvoyFilterTemplate,
		},
	}
	rr.Templates = append(rr.Templates, envoyFilterTemplate...)

	return nil
}

func createDestinationRule(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createDestinationRule")
	l.V(1).Info("Creating DestinationRule for TLS configuration")

	// Add DestinationRule template to the reconciliation request
	destinationRuleTemplate := []odhtypes.TemplateInfo{
		{
			FS:   gatewayResources,
			Path: DestinationRuleTemplate,
		},
	}
	rr.Templates = append(rr.Templates, destinationRuleTemplate...)

	return nil
}

func getTemplateData(ctx context.Context, rr *odhtypes.ReconciliationRequest) (map[string]any, error) {
	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return nil, errors.New("instance is not of type *services.GatewayConfig")
	}

	// Detect auth mode and get OIDC config
	authMode, err := detectClusterAuthMode(ctx, rr)
	if err != nil {
		return nil, fmt.Errorf("failed to detect cluster authentication mode: %w", err)
	}

	// Get domain for redirect URL, if not set in spec then fall back to cluster domain
	domain, err := getDomain(ctx, rr, gatewayConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve domain: %w", err)
	}

	isOIDC := authMode == AuthModeOIDC

	templateData := map[string]any{
		"GatewayNamespace":         gatewayNamespace,
		"GatewayName":              gatewayName,
		"KubeAuthProxyServiceName": kubeAuthProxyName,
		"KubeAuthProxyCredsSecret": kubeAuthProxyCredsSecret,
		"KubeAuthProxyTLSSecret":   kubeAuthProxyTLSSecret,
		"IsOIDC":                   isOIDC,
		"Domain":                   domain,
		"RedirectURL":              fmt.Sprintf("https://%s/oauth2/callback", domain),
	}

	// Add OIDC-specific data if needed
	if isOIDC && gatewayConfig.Spec.OIDC != nil {
		templateData["OIDCIssuerURL"] = gatewayConfig.Spec.OIDC.IssuerURL
		templateData["OIDCInsecureSkipVerify"] = false // TODO: need to check if this is correct or need more logic.
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
		Name:      gatewayName,
		Namespace: gatewayNamespace,
	}, gateway)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			rr.Conditions.MarkFalse(
				status.ConditionTypeReady,
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
			status.ConditionTypeReady,
			conditions.WithReason(status.ReadyReason),
			conditions.WithMessage(status.GatewayReadyMessage),
		)
	} else {
		rr.Conditions.MarkFalse(
			status.ConditionTypeReady,
			conditions.WithReason(status.NotReadyReason),
			conditions.WithMessage(status.GatewayNotReadyMessage),
		)
	}

	return nil
}
