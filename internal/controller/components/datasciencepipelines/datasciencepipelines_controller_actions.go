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

package datasciencepipelines

import (
	"context"
	"fmt"

	k8serr "k8s.io/apimachinery/pkg/api/errors"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odherr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

func checkPreConditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Conditions.MarkTrue(status.ConditionArgoWorkflowAvailable)

	crd, err := cluster.GetCRD(ctx, rr.Client, ArgoWorkflowCRD)
	switch {
	case k8serr.IsNotFound(err):
		return nil
	case err != nil:
		err = odherr.NewStopError("failed to check for existing %s CRD: %w", ArgoWorkflowCRD, err)

		rr.Conditions.MarkFalse(
			status.ConditionArgoWorkflowAvailable,
			conditions.WithObservedGeneration(rr.Instance.GetGeneration()),
			conditions.WithError(err),
		)

		return err
	}

	// Verify if existing workflow is deployed by ODH with label
	// if not then set Argo capability status condition to false
	odhLabelValue, odhLabelExists := crd.Labels[labels.ODH.Component(LegacyComponentName)]
	if !odhLabelExists || odhLabelValue != "true" {
		rr.Conditions.MarkFalse(
			status.ConditionArgoWorkflowAvailable,
			conditions.WithObservedGeneration(rr.Instance.GetGeneration()),
			conditions.WithReason(status.DataSciencePipelinesDoesntOwnArgoCRDReason),
			conditions.WithMessage(status.DataSciencePipelinesDoesntOwnArgoCRDMessage),
		)

		return ErrArgoWorkflowAPINotOwned
	}

	return nil
}

func initialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Manifests = append(rr.Manifests, manifestPath(rr.Release.Name))

	return nil
}

func devFlags(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	dsp, ok := rr.Instance.(*componentApi.DataSciencePipelines)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.DataSciencePipelines)", rr.Instance)
	}

	if dsp.Spec.DevFlags == nil {
		return nil
	}

	// Implement devflags support logic
	// If dev flags are set, update default manifests path
	if len(dsp.Spec.DevFlags.Manifests) != 0 {
		manifestConfig := dsp.Spec.DevFlags.Manifests[0]
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
