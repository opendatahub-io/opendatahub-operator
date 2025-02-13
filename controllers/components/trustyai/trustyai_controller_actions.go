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

package trustyai

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

func checkPreConditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	t, ok := rr.Instance.(*componentApi.TrustyAI)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.TrustyAI)", rr.Instance)
	}

	if err := cluster.CustomResourceDefinitionExists(ctx, rr.Client, gvk.InferenceServices.GroupKind()); err != nil {
		s := t.GetStatus()
		s.Phase = status.PhaseNotReady
		meta.SetStatusCondition(&s.Conditions, metav1.Condition{
			Type:               status.ConditionTypeReady,
			Status:             metav1.ConditionFalse,
			Reason:             status.ISVCMissingCRDReason,
			Message:            status.ISVCMissingCRDMessage,
			ObservedGeneration: s.ObservedGeneration,
		})
		return odherrors.NewStopError("failed to find InferenceService CRD: %v", err)
	}
	return nil
}

func initialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	rr.Manifests = append(rr.Manifests, manifestsPath(rr.Release.Name))
	return nil
}

func devFlags(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	trustyai, ok := rr.Instance.(*componentApi.TrustyAI)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.TrustyAI)", rr.Instance)
	}

	if trustyai.Spec.DevFlags == nil {
		return nil
	}

	// Implement devflags support logic
	// If dev flags are set, update default manifests path
	if len(trustyai.Spec.DevFlags.Manifests) != 0 {
		manifestConfig := trustyai.Spec.DevFlags.Manifests[0]
		if err := odhdeploy.DownloadManifests(ctx, ComponentName, manifestConfig); err != nil {
			return err
		}
		if manifestConfig.SourcePath != "" {
			rr.Manifests[0].SourcePath = manifestConfig.SourcePath
		}
	}

	return nil
}
