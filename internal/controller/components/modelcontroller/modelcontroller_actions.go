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

package modelcontroller

import (
	"context"
	"fmt"
	"strings"

	operatorv1 "github.com/openshift/api/operator/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

func initialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	// early exist
	mc, ok := rr.Instance.(*componentApi.ModelController)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelController)", rr.Instance)
	}
	rr.Manifests = append(rr.Manifests, manifestsPath())

	nimState := operatorv1.Removed
	if mc.Spec.Kserve.ManagementState == operatorv1.Managed {
		nimState = mc.Spec.Kserve.NIM.ManagementState
	}

	mrState := operatorv1.Removed
	if mc.Spec.ModelRegistry != nil && mc.Spec.ModelRegistry.ManagementState == operatorv1.Managed {
		mrState = operatorv1.Managed
	}

	extraParamsMap := map[string]string{
		"nim-state":              strings.ToLower(string(nimState)),
		"kserve-state":           strings.ToLower(string(mc.Spec.Kserve.ManagementState)),
		"modelmeshserving-state": strings.ToLower(string(mc.Spec.ModelMeshServing.ManagementState)),
		"modelregistry-state":    strings.ToLower(string(mrState)),
	}
	if err := odhdeploy.ApplyParams(rr.Manifests[0].String(), nil, extraParamsMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", rr.Manifests[0].String(), err)
	}

	return nil
}

// download devflag from kserve or modelmeshserving.
func devFlags(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	mc, ok := rr.Instance.(*componentApi.ModelController)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.ModelController)", rr.Instance)
	}

	l := logf.FromContext(ctx)

	var df *common.DevFlags

	ks := mc.Spec.Kserve
	ms := mc.Spec.ModelMeshServing

	switch {
	case ks != nil && ks.ManagementState == operatorv1.Managed && resources.HasDevFlags(ks):
		l.V(3).Info("Using DevFlags from KServe")
		df = ks.GetDevFlags()
	case ms != nil && ms.ManagementState == operatorv1.Managed && resources.HasDevFlags(ms):
		l.V(3).Info("Using DevFlags from ModelMesh")
		df = ms.GetDevFlags()
	default:
		return nil
	}

	for _, subcomponent := range df.Manifests {
		if !strings.Contains(subcomponent.URI, ComponentName) && !strings.Contains(subcomponent.URI, LegacyComponentName) {
			continue
		}

		l.V(3).Info("Downloading manifests", "uri", subcomponent.URI)

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
