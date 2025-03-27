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

package modelmeshserving

import (
	"context"
	"fmt"
	"strings"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

func initialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Manifests = append(rr.Manifests, manifestsPath())

	return nil
}

func devFlags(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	mm, ok := rr.Instance.(*componentApi.ModelMeshServing)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelMeshServing)", rr.Instance)
	}

	df := mm.GetDevFlags()
	if df == nil {
		return nil
	}
	if len(df.Manifests) == 0 {
		return nil
	}

	// Implement devflags support logic
	// If dev flags are set, update default manifests path
	for _, subcomponent := range df.Manifests {
		if !strings.Contains(subcomponent.URI, ComponentName) && !strings.Contains(subcomponent.URI, LegacyComponentName) {
			continue
		}

		// Download modelmeshserving
		if err := odhdeploy.DownloadManifests(ctx, ComponentName, subcomponent); err != nil {
			return err
		}
		// If overlay is defined, update paths
		if subcomponent.SourcePath != "" {
			rr.Manifests[0].SourcePath = subcomponent.SourcePath
		}

		break
	}

	return nil
}
