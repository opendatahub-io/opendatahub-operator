package feastoperator

import (
	"context"
	"errors"
	"fmt"
	"net/url"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	ComponentName = componentApi.FeastOperatorComponentName

	ReadyConditionType = componentApi.FeastOperatorKind + status.ReadySuffix

	// ParamsEnvKeyOIDCIssuerURL is the params.env key for the external OIDC issuer URL.
	// Feast kustomize uses SCREAMING_SNAKE_CASE for variables (same style as RELATED_IMAGE_*).
	ParamsEnvKeyOIDCIssuerURL = "OIDC_ISSUER_URL"
)

var (
	ManifestsSourcePath = map[common.Platform]string{
		cluster.SelfManagedRhoai: "overlays/rhoai",
		cluster.ManagedRhoai:     "overlays/rhoai",
		cluster.OpenDataHub:      "overlays/odh",
	}
	imageParamMap = map[string]string{
		"RELATED_IMAGE_FEAST_OPERATOR": "RELATED_IMAGE_ODH_FEAST_OPERATOR_IMAGE",
		"RELATED_IMAGE_FEATURE_SERVER": "RELATED_IMAGE_ODH_FEATURE_SERVER_IMAGE",
	}

	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
	}
)

func manifestPath(basePath string, p common.Platform) types.ManifestInfo {
	return types.ManifestInfo{
		Path:       basePath,
		ContextDir: ComponentName,
		SourcePath: ManifestsSourcePath[p],
	}
}

// parseAndValidateOIDCIssuerURL validates an OIDC issuer URL before persisting it on a CR or
// writing it to params.env: must be an absolute HTTPS URL with a host (same rules as
// setKustomizedParams). Returns the normalized form from url.URL.String().
func parseAndValidateOIDCIssuerURL(raw string) (string, error) {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return "", fmt.Errorf("parse OIDC issuer URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return "", errors.New("OIDC issuer URL must use https scheme")
	}
	if parsed.Host == "" {
		return "", errors.New("OIDC issuer URL must include a host")
	}
	return parsed.String(), nil
}

// getGatewayOIDCSpec returns a GatewayOIDCSpec with the OIDC issuer when the cluster uses external
// OIDC and GatewayConfig lists an issuer URL. Returns nil when OpenShift OAuth is used, when
// GatewayConfig is missing, or when OIDC is not yet configured. Returns an error if the issuer URL
// is invalid.
func getGatewayOIDCSpec(ctx context.Context, cli client.Client) (*common.GatewayOIDCSpec, error) {
	authMode, err := cluster.GetClusterAuthenticationMode(ctx, cli)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to detect cluster authentication mode: %w", err)
	}

	if authMode != cluster.AuthModeOIDC {
		return nil, nil
	}

	gc := serviceApi.GatewayConfig{}
	if err := cli.Get(ctx, client.ObjectKey{Name: serviceApi.GatewayConfigName}, &gc); err != nil {
		if k8serr.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get GatewayConfig: %w", err)
	}

	if gc.Spec.OIDC == nil || gc.Spec.OIDC.IssuerURL == "" {
		return nil, nil
	}

	normalized, err := parseAndValidateOIDCIssuerURL(gc.Spec.OIDC.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("%s has invalid spec.oidc.issuerURL: %w", serviceApi.GatewayConfigName, err)
	}

	return &common.GatewayOIDCSpec{IssuerURL: normalized}, nil
}
