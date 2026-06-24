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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	deploymentName     = "trustyai-service-operator-controller-manager"
	selectorLabelKey   = "app.kubernetes.io/part-of"
	selectorLabelValue = "trustyai"
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
	trustyai, ok := rr.Instance.(*componentApi.TrustyAI)
	if !ok {
		return fmt.Errorf("resource instance %v is not a *componentApi.TrustyAI", rr.Instance)
	}

	// Add MCP Guardrails overlay if MCPGuardrailsMode is enabled
	if trustyai.Spec.MCPGuardrailsMode {
		rr.Manifests = append(rr.Manifests, mcpGuardrailsManifestInfo(rr.ManifestsBasePath))
	} else {
		rr.Manifests = append(rr.Manifests, manifestsPath(rr.ManifestsBasePath, rr.Release.Name))
	}

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
		return err
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

// migrateDeploymentSelector deletes the trustyai-service-operator-controller-manager Deployment if its
// spec.selector.matchLabels is missing the app.kubernetes.io/part-of label. This handles upgrades
// where the selector changed between releases — since spec.selector is immutable on Deployments,
// the only way to update it is to delete and let the operator recreate it.
func migrateDeploymentSelector(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	log := logf.FromContext(ctx)

	ns, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return fmt.Errorf("failed to determine application namespace: %w", err)
	}

	deploy := &appsv1.Deployment{}
	err = rr.Client.Get(ctx, client.ObjectKey{Name: deploymentName, Namespace: ns}, deploy)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get Deployment %s/%s: %w", ns, deploymentName, err)
	}

	if deploy.Spec.Selector == nil {
		return nil
	}

	if deploy.Spec.Selector.MatchLabels[selectorLabelKey] == selectorLabelValue {
		return nil
	}

	log.Info("TrustyAI Deployment has stale selector, deleting for recreation",
		"deployment", deploymentName,
		"namespace", ns,
		"hasExpectedLabel", deploy.Spec.Selector.MatchLabels[selectorLabelKey] != "",
	)

	if err := rr.Client.Delete(ctx, deploy); err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to delete Deployment %s/%s with stale selector: %w", ns, deploymentName, err)
	}

	log.Info("Deleted TrustyAI Deployment, it will be recreated with the correct selector",
		"deployment", deploymentName, "namespace", ns)

	return nil
}
