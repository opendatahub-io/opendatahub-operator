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

func createConfigMap(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	trustyai, ok := rr.Instance.(*componentApi.TrustyAI)
	if !ok {
		return fmt.Errorf("resource instance %v is not a componentApi.TrustyAI)", rr.Instance)
	}

	// Fetch application namespace from DSCI.
	appNamespace, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return fmt.Errorf("failed to get applications namespace: %w", err)
	}

	// Convert to boolean for configmap
	permitCodeExecution := trustyai.Spec.Eval.LMEval.PermitCodeExecution == EvalPermissionAllow
	permitOnline := trustyai.Spec.Eval.LMEval.PermitOnline == EvalPermissionAllow

	// Create extra ConfigMap for DSC configuration
	configMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gvk.ConfigMap.Version,
			Kind:       gvk.ConfigMap.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			// TrustyAI's own default ConfigMap name is "trustyai-service-operator-config"
			Name:      "trustyai-dsc-config",
			Namespace: appNamespace,
		},
		Data: make(map[string]string),
	}

	configMap.Data["eval.lmeval.permitCodeExecution"] = strconv.FormatBool(permitCodeExecution)
	configMap.Data["eval.lmeval.permitOnline"] = strconv.FormatBool(permitOnline)

	return rr.AddResources(configMap)
}
