package llamastackoperator

import (
	"context"

	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func initialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error { //nolint:unparam
	rr.Manifests = append(rr.Manifests, manifestPath(rr.ManifestsBasePath, rr.Release.Name))
	return nil
}
