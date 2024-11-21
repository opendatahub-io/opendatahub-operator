package codeflare

import (
	"context"
	"fmt"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Manifests = append(rr.Manifests, odhtypes.ManifestInfo{
		Path:       DefaultPath,
		ContextDir: "",
		SourcePath: "",
	})
	if err := odhdeploy.ApplyParams(DefaultPath, nil, map[string]string{"namespace": rr.DSCI.Spec.ApplicationsNamespace}); err != nil {
		return fmt.Errorf("failed to update params.env from %s : %w", rr.Manifests[0], err)
	}
	return nil
}

func devFlags(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	codeflare, ok := rr.Instance.(*componentsv1.CodeFlare)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentsv1.CodeFlare)", rr.Instance)
	}

	if codeflare.Spec.DevFlags == nil {
		return nil
	}
	// Implement devflags support logic
	// If dev flags are set, update default manifests path
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

	return nil
}
