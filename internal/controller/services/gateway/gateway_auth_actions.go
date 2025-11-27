package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/serializer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

func createKubeAuthProxyInfrastructure(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	l := logf.FromContext(ctx).WithName("createAuthProxy")

	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return errors.New("instance is not of type *services.GatewayConfig")
	}

	l.V(1).Info("creating auth proxy for gateway", "gateway", gatewayConfig.Name)

	// Resolve domain consistently with createGatewayInfrastructure
	domain, err := GetFQDN(ctx, rr.Client, gatewayConfig)
	if err != nil {
		return fmt.Errorf("failed to resolve domain: %w", err)
	}

	authMode, err := cluster.GetClusterAuthenticationMode(ctx, rr.Client)
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
	if authMode == cluster.AuthModeOIDC {
		oidcConfig = gatewayConfig.Spec.OIDC
	}

	// get or generate secrets for kube-auth-proxy (handles OAuth and OIDC modes)
	clientSecret, cookieSecret, err := getOrGenerateSecrets(ctx, rr, authMode)
	if err != nil {
		return fmt.Errorf("failed to get or generate secrets: %w", err)
	}

	if err := deployKubeAuthProxy(ctx, rr, oidcConfig, &gatewayConfig.Spec.Cookie, gatewayConfig.Spec.NetworkPolicy, clientSecret, cookieSecret, domain); err != nil {
		return fmt.Errorf("failed to deploy auth proxy: %w", err)
	}

	if authMode == cluster.AuthModeIntegratedOAuth {
		if err := createOAuthClient(ctx, rr, clientSecret); err != nil {
			return fmt.Errorf("failed to create OAuth client: %w", err)
		}
	}

	if err := createOAuthCallbackRoute(rr); err != nil {
		return fmt.Errorf("failed to create OAuth callback route: %w", err)
	}

	return nil
}

// getGatewayAuthTimeout returns the auth timeout using:
// Deprecated AuthTimeout field > AuthProxyTimeout field > default (5s).
func getGatewayAuthTimeout(gatewayConfig *serviceApi.GatewayConfig) string {
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

func createEnvoyFilter(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return errors.New("instance is not of type *services.GatewayConfig")
	}

	authTimeout := getGatewayAuthTimeout(gatewayConfig)

	// using yaml templates due to complexity of k8s api struct for envoy filter
	yamlContent, err := gatewayResources.ReadFile(envoyFilterTemplate)
	if err != nil {
		return fmt.Errorf("failed to read EnvoyFilter template: %w", err)
	}

	yamlString := string(yamlContent)
	yamlString = fmt.Sprintf(yamlString, authTimeout, authTimeout)
	yamlString = strings.ReplaceAll(yamlString, "{{.CookieName}}", OAuth2ProxyCookieName)

	decoder := serializer.NewCodecFactory(rr.Client.Scheme()).UniversalDeserializer()
	unstructuredObjects, err := resources.Decode(decoder, []byte(yamlString))
	if err != nil {
		return fmt.Errorf("failed to decode EnvoyFilter YAML: %w", err)
	}

	if len(unstructuredObjects) != 1 {
		return fmt.Errorf("expected exactly 1 EnvoyFilter object, got %d", len(unstructuredObjects))
	}

	return rr.AddResources(&unstructuredObjects[0])
}
