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

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

func checkPreConditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	dsp, ok := rr.Instance.(*componentApi.DataSciencePipelines)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.DataSciencePipelines", rr.Instance)
	}

	workflowCRD := &apiextensionsv1.CustomResourceDefinition{}
	if err := rr.Client.Get(ctx, client.ObjectKey{Name: ArgoWorkflowCRD}, workflowCRD); err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}
		return odherrors.NewStopError("failed to get existing Workflow CRD : %v", err)
	}

	// Verify if existing workflow is deployed by ODH with label
	// if not then set Argo capability status condition to false
	odhLabelValue, odhLabelExists := workflowCRD.Labels[labels.ODH.Component(LegacyComponentName)]
	if !odhLabelExists || odhLabelValue != "true" {
		s := dsp.GetStatus()
		s.Phase = "NotReady"

		meta.SetStatusCondition(&s.Conditions, metav1.Condition{
			Type:               status.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             status.DataSciencePipelinesDoesntOwnArgoCRDReason,
			Message:            status.DataSciencePipelinesDoesntOwnArgoCRDMessage,
			ObservedGeneration: s.ObservedGeneration,
		})

		return odherrors.NewStopError(status.DataSciencePipelinesDoesntOwnArgoCRDMessage)
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
