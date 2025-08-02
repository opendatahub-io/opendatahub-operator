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
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

func checkPreConditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	isvc, err := cluster.HasCRD(ctx, rr.Client, gvk.InferenceServices)
	if err != nil {
		return odherrors.NewStopError("failed to check %s CRDs version: %w", gvk.InferenceServices, err)
	}

	if !isvc {
		return odherrors.NewStopError(status.ISVCMissingCRDMessage)
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

func createConfigMap(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	trustyai, ok := rr.Instance.(*componentApi.TrustyAI)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.TrustyAI)", rr.Instance)
	}

	// Skip ConfigMap creation if no configuration is specified
	if trustyai.Spec.Eval.LMEval.AllowCodeExecution == nil &&
		trustyai.Spec.Eval.LMEval.AllowOnline == nil {
		return nil
	}

	// Create extra ConfigMap for DSC configuration
	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gvk.ConfigMap.Version,
			Kind:       gvk.ConfigMap.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			// TrustyAI's own default ConfigMap name is "trustyai-service-operator-config"
			Name:      "trustyai-dsc-config",
			Namespace: rr.DSCI.Spec.ApplicationsNamespace,
			Labels: map[string]string{
				labels.ODH.Component(ComponentName): labels.True,
				labels.K8SCommon.PartOf:             ComponentName,
				"app.opendatahub.io/config-type":    "dsc-config",
			},
			Annotations: map[string]string{
				"opendatahub.io/managed-by":    "dsc-trustyai-controller",
				"opendatahub.io/config-source": "datasciencecluster",
			},
		},
		Data: make(map[string]string),
	}

	if trustyai.Spec.Eval.LMEval.AllowCodeExecution != nil {
		configMap.Data["eval.lmeval.allowCodeExecution"] =
			strconv.FormatBool(*trustyai.Spec.Eval.LMEval.AllowCodeExecution)
	}
	if trustyai.Spec.Eval.LMEval.AllowOnline != nil {
		configMap.Data["eval.lmeval.allowOnline"] =
			strconv.FormatBool(*trustyai.Spec.Eval.LMEval.AllowOnline)
	}

	return rr.Client.Patch(ctx, configMap, client.Apply,
		client.ForceOwnership, client.FieldOwner("trustyai-dsc-controller"))
}
