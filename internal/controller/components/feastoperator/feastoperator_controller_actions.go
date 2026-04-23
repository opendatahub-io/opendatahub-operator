package feastoperator

import (
	"context"
	"errors"
	"fmt"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

func initialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error { //nolint:unparam
	rr.Manifests = append(rr.Manifests, manifestPath(rr.ManifestsBasePath, rr.Release.Name))
	return nil
}

// setKustomizedParams merges runtime values from the FeastOperator CR into params.env before kustomize
// (same role as dashboard's setKustomizedParams). Add future keys to extraParams as needed.
func setKustomizedParams(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	feast, ok := rr.Instance.(*componentApi.FeastOperator)
	if !ok {
		return errors.New("instance is not a FeastOperator")
	}

	if len(rr.Manifests) == 0 {
		return errors.New("no manifests initialized before setKustomizedParams")
	}

	issuerURL := ""
	if feast.Spec.OIDC != nil {
		var err error
		issuerURL, err = parseAndValidateOIDCIssuerURL(feast.Spec.OIDC.IssuerURL)
		if err != nil {
			return fmt.Errorf("invalid OIDC issuer URL %q in FeastOperator spec.oidc: %w", feast.Spec.OIDC.IssuerURL, err)
		}
	}

	extraParams := map[string]string{
		ParamsEnvKeyOIDCIssuerURL: issuerURL,
	}

	if err := odhdeploy.ApplyParams(rr.Manifests[0].String(), "params.env", nil, extraParams); err != nil {
		return fmt.Errorf("failed to update params.env with kustomize parameters: %w", err)
	}

	return nil
}
