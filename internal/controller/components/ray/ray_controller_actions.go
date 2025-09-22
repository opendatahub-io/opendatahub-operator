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

package ray

import (
	"context"
	"fmt"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Manifests = append(rr.Manifests, manifestPath())

	if err := odhdeploy.ApplyParams(manifestPath().String(), "params.env", nil, map[string]string{"namespace": rr.DSCI.Spec.ApplicationsNamespace}); err != nil {
		return fmt.Errorf("failed to update params.env from %s : %w", manifestPath(), err)
	}
	return nil
}

func devFlags(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	ray, ok := rr.Instance.(*componentApi.Ray)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.Ray)", rr.Instance)
	}

	if ray.Spec.DevFlags == nil {
		return nil
	}
	// Implement devflags support logic
	// If dev flags are set, update default manifests path
	if len(ray.Spec.DevFlags.Manifests) != 0 {
		manifestConfig := ray.Spec.DevFlags.Manifests[0]
		if err := odhdeploy.DownloadManifests(ctx, ComponentName, manifestConfig); err != nil {
			return err
		}
		if manifestConfig.SourcePath != "" {
			rr.Manifests[0].Path = odhdeploy.DefaultManifestPath
			rr.Manifests[0].ContextDir = ComponentName
			rr.Manifests[0].SourcePath = manifestConfig.SourcePath
		}
	}
	// TODO: Implement devflags logmode logic
	return nil
}

// This function is used to perform the sanity checks for the Ray component upgrade.
// The configuration is ok if CodeFlare component resource is not present in the cluster,
// as it is required to remove it before upgrade to ODH v3.
// If CRD is not present, sanity check passes.
func performV3UpgradeSanityChecks(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	hasCrd, err := cluster.HasCRD(ctx, rr.Client, gvk.CodeFlare)
	if err != nil {
		return fmt.Errorf("failed to check if CodeFlare CRD exists: %w", err)
	}

	if !hasCrd {
		return nil
	}

	codeFlareResources, err := cluster.ListGVK(ctx, rr.Client, gvk.CodeFlare)
	if err != nil {
		return fmt.Errorf("failed to list CodeFlare resources: %w", err)
	}

	// If we found any CodeFlare resources, sanity check failed
	if len(codeFlareResources) > 0 {
		return odherrors.NewStopError(status.CodeFlarePresentMessage)
	}

	return nil
}
