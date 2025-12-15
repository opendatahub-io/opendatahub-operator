/*
Copyright 2025.

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

package mlflowoperator

import (
	"context"
	"fmt"
	"path"

	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

func initialize(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Manifests = append(rr.Manifests, manifestPath(rr.Release.Name))
	return nil
}

func setKustomizedParams(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	extraParamsMap, err := ComputeKustomizeVariable(ctx, rr.Client, rr.Release.Name)
	if err != nil {
		return fmt.Errorf("failed to set variable for url, section-title etc: %w", err)
	}

	paramsPath := path.Join(odhdeploy.DefaultManifestPath, ComponentName, "base")

	if err := odhdeploy.ApplyParams(paramsPath, "params.env", nil, extraParamsMap); err != nil {
		return fmt.Errorf("failed to update params.env from %s : %w", paramsPath, err)
	}
	return nil
}
