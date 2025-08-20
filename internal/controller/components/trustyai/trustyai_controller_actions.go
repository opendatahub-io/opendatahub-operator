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

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
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
		},
		Data: make(map[string]string),
	}

	configMap.Data["eval.lmeval.permitCodeExecution"] =
		strconv.FormatBool(trustyai.Spec.Eval.LMEval.PermitCodeExecution)
	configMap.Data["eval.lmeval.permitOnline"] =
		strconv.FormatBool(trustyai.Spec.Eval.LMEval.PermitOnline)

	return rr.AddResources(configMap)
}
