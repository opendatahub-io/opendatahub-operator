package codeflare

import (
	"context"
	"fmt"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	codeflare, ok := rr.Instance.(*componentApi.CodeFlare)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.CodeFlare)", rr.Instance)
	}

	rr.Manifests = append(rr.Manifests, manifestsPath())

	// Implement devflags support logic
	// If dev flags are set, update default manifests path
	if codeflare.GetDevFlags() != nil {
		if len(codeflare.Spec.DevFlags.Manifests) != 0 {
			manifestConfig := codeflare.Spec.DevFlags.Manifests[0]
			if err := odhdeploy.DownloadManifests(ctx, ComponentName, manifestConfig); err != nil {
				return err
			}
			if manifestConfig.SourcePath != "" {
				rr.Manifests[0].Path = odhdeploy.DefaultManifestPath
				rr.Manifests[0].ContextDir = ComponentName
				rr.Manifests[0].SourcePath = manifestConfig.SourcePath
			}
		}
	}

	if err := odhdeploy.ApplyParams(
		paramsPath,
		nil,
		map[string]string{"namespace": rr.DSCI.Spec.ApplicationsNamespace},
	); err != nil {
		return fmt.Errorf("failed to update params.env from %s : %w", paramsPath, err)
	}
	return nil
}
