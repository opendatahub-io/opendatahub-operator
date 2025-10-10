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

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
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
		"hostname", hostname,
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

	// Add KubeAuthProxy templates to the reconciliation request
	kubeAuthProxyTemplates := []odhtypes.TemplateInfo{
		{
			FS:   gatewayResources,
			Path: kubeAuthProxyDeploymentTemplate,
		},
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
	rr.Templates = append(rr.Templates, kubeAuthProxyTemplates...)

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

func createComponentHttpRoute(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createComponentHttpRoute")
	l.V(1).Info("Creating HTTPRoute for components")

	// Define component types to check for with their API instance names
	componentChecks := map[string]struct {
		shouldCreateHttproute func() bool
		instanceName          string
	}{ // TODO: extend this list for other components.
		"dashboard": {
			shouldCreateHttproute: func() bool {
				dashboard := &componentApi.Dashboard{}
				dashboard.Name = componentApi.DashboardInstanceName
				err := rr.Client.Get(ctx, client.ObjectKeyFromObject(dashboard), dashboard)
				return err == nil
			},
			instanceName: componentApi.DashboardInstanceName,
		},
	}

	var componentHttpRouteTemplate []odhtypes.TemplateInfo

	// Check each component and add template if CR exists
	for component, config := range componentChecks {
		if config.shouldCreateHttproute() {
			l.V(1).Info("Component CR exists, adding HTTPRoute template", "component", component, "instanceName", config.instanceName)
			componentHttpRouteTemplate = append(componentHttpRouteTemplate, odhtypes.TemplateInfo{
				FS:   gatewayResources,
				Path: componentHttpRouteTemplatePath + "/" + config.instanceName + ".tmpl.yaml",
			})
		} else {
			l.V(1).Info("Component CR does not exist, skipping HTTPRoute template", "component", component)
		}
	}

	// add dns-map-route template even it is often on PSI.
	dnsMapRouteTemplate := odhtypes.TemplateInfo{
		FS:   gatewayResources,
		Path: componentHttpRouteTemplatePath + "/dns-map-route.tmpl.yaml",
	}
	componentHttpRouteTemplate = append(componentHttpRouteTemplate, dnsMapRouteTemplate)

	rr.Templates = append(rr.Templates, componentHttpRouteTemplate...)
	return nil
}

func getTemplateData(ctx context.Context, rr *odhtypes.ReconciliationRequest) (map[string]any, error) {
	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return nil, errors.New("instance is not of type *services.GatewayConfig")
	}

	if rr.DSCI == nil {
		return nil, errors.New("failed to get DSCI to retrieve application namespace")
	}

	// Detect auth mode and get OIDC config
	authMode, err := cluster.GetClusterAuthenticationMode(ctx, rr.Client)
	if err != nil {
		return nil, fmt.Errorf("failed to detect cluster authentication mode: %w", err)
	}

	// Get domain for redirect URL, if not set in spec then fall back to cluster domain
	hostname, err := GetFQDN(ctx, rr.Client, gatewayConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve domain: %w", err)
	}

	isOIDC := authMode == cluster.AuthModeOIDC

	templateData := map[string]any{
		"GatewayNamespace":         GatewayNamespace,
		"GatewayName":              DefaultGatewayName,
		"GatewayClassName":         GatewayClassName,
		"GatewayHostname":          hostname,
		"ApplicationNamespace":     rr.DSCI.Spec.ApplicationsNamespace,
		"KubeAuthProxyServiceName": KubeAuthProxyName,
		"KubeAuthProxySecretsName": KubeAuthProxySecretsName,
		"KubeAuthProxyTLSName":     KubeAuthProxyTLSName,
		"OAuthCallbackRouteName":   OAuthCallbackRouteName,
		"KubeAuthProxyImage":       GetKubeAuthProxyImage(),
		"AuthProxyHTTPPort":        AuthProxyHTTPPort,
		"StandardHTTPSPort":        StandardHTTPSPort,
		"GatewayHTTPSPort":         GatewayHTTPSPort,
		"AuthProxyOAuth2Path":      AuthProxyOAuth2Path,
		"TLSCertsVolumeName":       TLSCertsVolumeName,
		"TLSCertsMountPath":        TLSCertsMountPath,
		"EnvoyFilter":              AuthnFilterName,
		"IsOIDC":                   isOIDC,
		"RedirectURL":              fmt.Sprintf("https://%s/oauth2/callback", hostname),
		"DestinationRuleName":      DestinationRuleName,
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
	} else {
		rr.Conditions.MarkFalse(
			ReadyConditionType,
			conditions.WithReason(status.NotReadyReason),
			conditions.WithMessage(status.GatewayNotReadyMessage),
		)
	}

	return nil
}
