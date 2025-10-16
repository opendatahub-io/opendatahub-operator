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
	"encoding/json"
	"fmt"
	"path"

	operatorv1 "github.com/openshift/api/operator/v1"
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
	dsp, ok := rr.Instance.(*componentApi.DataSciencePipelines)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.DataSciencePipelines)", rr.Instance)
	}

	awfSpec := dsp.Spec.ArgoWorkflowsControllers
	awfRemoved := awfSpec != nil && awfSpec.ManagementState == operatorv1.Removed

	rr.Conditions.MarkTrue(status.ConditionArgoWorkflowAvailable)

	crd, err := cluster.GetCRD(ctx, rr.Client, ArgoWorkflowCRD)
	switch {
	case k8serr.IsNotFound(err):
		if awfRemoved {
			rr.Conditions.MarkFalse(
				status.ConditionArgoWorkflowAvailable,
				conditions.WithObservedGeneration(rr.Instance.GetGeneration()),
				conditions.WithReason(status.DataSciencePipelinesArgoWorkflowsCRDMissingReason),
				conditions.WithMessage(status.DataSciencePipelinesArgoWorkflowsCRDMissingMessage),
			)

			return ErrArgoWorkflowCRDMissing
		}

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

	if awfRemoved {
		rr.Conditions.MarkTrue(status.ConditionArgoWorkflowAvailable,
			conditions.WithObservedGeneration(rr.Instance.GetGeneration()),
			conditions.WithReason(status.DataSciencePipelinesArgoWorkflowsNotManagedReason),
			conditions.WithMessage(status.DataSciencePipelinesArgoWorkflowsNotManagedMessage),
		)

		return nil
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

func argoWorkflowsControllersOptions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	dsp, ok := rr.Instance.(*componentApi.DataSciencePipelines)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.DataSciencePipelines)", rr.Instance)
	}

	awfSpec := dsp.Spec.ArgoWorkflowsControllers

	if awfSpec == nil {
		awfSpec = &componentApi.ArgoWorkflowsControllersSpec{
			ManagementState: operatorv1.Managed,
		}
	}

	awfSpecJSON, err := json.Marshal(awfSpec)
	if err != nil {
		return fmt.Errorf("failed to marshal spec.argoWorkflowsControllers: %w", err)
	}

	extraParams := map[string]string{
		argoWorkflowsControllersParamsKey: string(awfSpecJSON),
	}

	paramsPath := path.Join(odhdeploy.DefaultManifestPath, ComponentName, "base")

	if err := odhdeploy.ApplyParams(paramsPath, "params.env", imageParamMap, extraParams); err != nil {
		return fmt.Errorf("failed to update params.env: %w", err)
	}

	return nil
}
