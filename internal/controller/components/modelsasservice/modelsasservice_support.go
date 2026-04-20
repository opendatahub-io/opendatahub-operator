/*
Copyright 2025.

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

package modelsasservice

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/plugins"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	ComponentName = componentApi.ModelsAsServiceComponentName

	ReadyConditionType = "Tenant" + status.ReadySuffix

	// Default Gateway values as specified in the spec.
	DefaultGatewayNamespace = "openshift-ingress"
	DefaultGatewayName      = "maas-default-gateway"

	// MaaSSubscriptionNamespace is the namespace where MaaS CRs live
	// (Tenant, MaaSSubscription, MaaSAuthPolicy). Must match the
	// maas-controller --maas-subscription-namespace flag.
	MaaSSubscriptionNamespace = "models-as-a-service"

	// Manifest paths.
	BaseManifestsSourcePath = "overlays/odh"
)

var (
	// Image parameter mappings for manifest substitution.
	imagesMap = map[string]string{
		"maas-api-image":             "RELATED_IMAGE_ODH_MAAS_API_IMAGE",
		"maas-controller-image":      "RELATED_IMAGE_ODH_MAAS_CONTROLLER_IMAGE",
		"payload-processing-image":   "RELATED_IMAGE_ODH_AI_GATEWAY_PAYLOAD_PROCESSING_IMAGE",
		"maas-api-key-cleanup-image": "RELATED_IMAGE_UBI_MINIMAL_IMAGE",
	}

	// Additional parameters for manifest customization.
	extraParamsMap = map[string]string{
		"gateway-namespace": DefaultGatewayNamespace,
		"gateway-name":      DefaultGatewayName,
	}
)

func baseManifestInfo(basePath string, sourcePath string) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       basePath,
		ContextDir: "maas",
		SourcePath: sourcePath,
	}
}

// AppendOperatorInstallManifests renders the maas-controller kustomize bundle and prepends it to
// rr.Resources so the DataScienceCluster deploy action applies it with the same ownership model
// as other DSC-managed resources. Call only when Models-as-a-Service is enabled for the DSC
// (e.g. registry IsComponentEnabled(ModelsAsServiceComponentName)); platform reconcile for
// Tenant remains in maas-controller.
func AppendOperatorInstallManifests(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	root := rr.ManifestsBasePath
	if root == "" {
		return errors.New("ManifestsBasePath is unset; cannot render maas-controller install bundle")
	}

	kPath := filepath.Join(root, "maas", "base", "maas-controller", "default")
	if _, err := os.Stat(filepath.Join(kPath, "kustomization.yaml")); err != nil {
		return fmt.Errorf("maas-controller install bundle not found at %q: %w", kPath, err)
	}

	appNs, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return fmt.Errorf("application namespace for maas-controller install: %w", err)
	}

	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	fs := filesys.MakeFsOnDisk()
	resMap, err := k.Run(fs, kPath)
	if err != nil {
		return fmt.Errorf("kustomize build %q: %w", kPath, err)
	}

	if err := plugins.CreateNamespaceApplierPlugin(appNs).Transform(resMap); err != nil {
		return fmt.Errorf("namespace transform for maas-controller bundle: %w", err)
	}

	componentLabels := map[string]string{
		labels.ODH.Component(componentApi.ModelsAsServiceComponentName): labels.True,
		labels.K8SCommon.PartOf: componentApi.ModelsAsServiceComponentName,
	}
	if err := plugins.CreateSetLabelsPlugin(componentLabels).Transform(resMap); err != nil {
		return fmt.Errorf("labels transform for maas-controller bundle: %w", err)
	}

	rendered := resMap.Resources()
	extra := make([]unstructured.Unstructured, 0, len(rendered)+1)
	for i := range rendered {
		m, err := rendered[i].Map()
		if err != nil {
			return fmt.Errorf("maas-controller bundle resource map: %w", err)
		}
		// Kustomize map values may include Go int; deploy Hash→DeepCopy cannot copy int
		// (runtime.DeepCopyJSONValue). JSON round-trip yields float64 for numbers.
		m, err = normalizeUnstructuredObject(m)
		if err != nil {
			return fmt.Errorf("normalize maas-controller bundle object: %w", err)
		}
		extra = append(extra, unstructured.Unstructured{Object: m})
	}

	paramsCM, err := maasParametersConfigMapFromParamsEnv(root, appNs, componentLabels)
	if err != nil {
		return fmt.Errorf("build maas-parameters ConfigMap from params.env: %w", err)
	}
	extra = append(extra, *paramsCM)

	sortedExtra, err := resources.SortByApplyOrder(ctx, extra)
	if err != nil {
		return fmt.Errorf("sort maas-controller install bundle: %w", err)
	}

	// CRDs and namespaced operator resources must apply before Tenant and other component CRs.
	rr.Resources = append(sortedExtra, rr.Resources...)
	return nil
}

// maasParametersConfigMapFromParamsEnv reads the already-updated params.env
// (Init → ApplyParams has already merged RELATED_IMAGE_* and extraParamsMap)
// and builds the maas-parameters ConfigMap that is deployed alongside
// maas-controller. This is the authoritative source of maas-parameters;
// the Tenant reconciler consumes it rather than regenerating it.
func maasParametersConfigMapFromParamsEnv(manifestsBasePath string, appNs string, componentLabels map[string]string) (*unstructured.Unstructured, error) {
	paramsFile := filepath.Join(manifestsBasePath, "maas", BaseManifestsSourcePath, "params.env")
	paramsMap, err := parseParamsEnv(paramsFile)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", paramsFile, err)
	}

	data := make(map[string]interface{}, len(paramsMap))
	for k, v := range paramsMap {
		data[k] = v
	}

	cm := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      "maas-parameters",
				"namespace": appNs,
				"labels":    toStringInterfaceMap(componentLabels),
			},
			"data": data,
		},
	}
	return cm, nil
}

// parseParamsEnv reads a key=value env file, skipping comments and blank lines.
func parseParamsEnv(filename string) (map[string]string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	m := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if key, value, ok := strings.Cut(line, "="); ok {
			m[key] = value
		}
	}
	return m, scanner.Err()
}

func toStringInterfaceMap(m map[string]string) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func normalizeUnstructuredObject(obj map[string]interface{}) (map[string]interface{}, error) {
	b, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}
