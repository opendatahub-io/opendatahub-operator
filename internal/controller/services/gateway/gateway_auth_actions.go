package gateway

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/serializer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
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
	domain, err := resolveDomain(ctx, rr.Client, gatewayConfig)
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
