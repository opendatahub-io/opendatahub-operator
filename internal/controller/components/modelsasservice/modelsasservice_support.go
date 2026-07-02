/*
Copyright 2026.

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
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/kustomize/api/builtins" //nolint:staticcheck // Remove after package update
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/api/resource"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/filesys"
	kyaml "sigs.k8s.io/kustomize/kyaml/yaml"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/plugins"
)

const (
	ComponentName = componentApi.ModelsAsServiceComponentName

	ReadyConditionType = componentApi.ModelsAsServiceKind + status.ReadySuffix

	// Default Gateway values as specified in the spec.
	DefaultGatewayNamespace = "openshift-ingress"
	DefaultGatewayName      = "maas-default-gateway"

	// MaaSSubscriptionNamespace is the namespace where MaaS CRs live
	// (Tenant, MaaSSubscription, MaaSAuthPolicy). Must match the
	// maas-controller --maas-subscription-namespace flag.
	MaaSSubscriptionNamespace = "models-as-a-service"

	// MaasControllerDeploymentName is the maas-controller workload Deployment name in the
	// application namespace (must match upstream manager kustomize).
	MaasControllerDeploymentName = "maas-controller"

	// Manifest paths.
	BaseManifestsSourcePath = "overlays/odh"

	// MaasManifestContextDir is the kustomize bundle directory under ManifestsBasePath (matches
	// baseManifestInfo ContextDir).
	MaasManifestContextDir = "maas"

	// MaasClusterConfigName is the cluster-scoped maas Config singleton name (must match
	// maas-controller; see models-as-a-service lifecycle).
	MaasClusterConfigName = "default"
)

var (
	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
	}

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
		ContextDir: MaasManifestContextDir,
		SourcePath: sourcePath,
	}
}

// buildMaasOperatorInstallManifests renders the maas-controller kustomize bundle (CRDs, RBAC,
// Deployment, maas-parameters ConfigMap). Used by the ModelsAsService component reconciler so
// workloads get controller ownership from the ModelsAsService CR; Tenant CR lifecycle remains
// in maas-controller. Cluster-scoped maas Config is not in this bundle (Lifecycle in maas-controller
// owns that CR); the ModelsAsService reconciler patches controller ownership on Config separately.
// Do not call from the DataScienceCluster reconciler: deploy would set owner references on the DSC
// instance instead of the ModelsAsService CR.
func buildMaasOperatorInstallManifests(ctx context.Context, rr *odhtypes.ReconciliationRequest) ([]client.Object, error) {
	root := rr.ManifestsBasePath
	if root == "" {
		return nil, errors.New("ManifestsBasePath is unset; cannot render maas-controller install bundle")
	}

	mi := baseManifestInfo(root, BaseManifestsSourcePath)
	kPath := filepath.Join(mi.Path, mi.ContextDir, "base", "maas-controller", "default")
	if _, err := os.Stat(filepath.Join(kPath, "kustomization.yaml")); err != nil {
		return nil, fmt.Errorf("maas-controller install bundle not found at %q: %w", kPath, err)
	}

	appNs, err := cluster.ApplicationNamespace(ctx, rr.Client)
	if err != nil {
		return nil, fmt.Errorf("application namespace for maas-controller install: %w", err)
	}

	monitoringNs, err := cluster.MonitoringNamespace(ctx, rr.Client)
	if err != nil {
		return nil, fmt.Errorf("monitoring namespace for maas-controller install: %w", err)
	}
	if monitoringNs == "" {
		return nil, errors.New("monitoring namespace cannot be empty")
	}
	if errs := validation.IsDNS1123Label(monitoringNs); len(errs) > 0 {
		return nil, fmt.Errorf("monitoring namespace %q is not a valid DNS-1123 label: %v", monitoringNs, errs)
	}

	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	fs := filesys.MakeFsOnDisk()
	resMap, err := k.Run(fs, kPath)
	if err != nil {
		return nil, fmt.Errorf("kustomize build %q: %w", kPath, err)
	}

	if err := plugins.CreateNamespaceApplierPlugin(appNs).Transform(resMap); err != nil {
		return nil, fmt.Errorf("namespace transform for maas-controller bundle: %w", err)
	}

	// The blanket namespace transform above moves ALL resources to appNs, but
	// payload-processing resources must remain in the gateway namespace because
	// the EnvoyFilter must run in the same namespace as the Gateway for Envoy
	// to attach ext_proc filters. Restore their namespace to the gateway ns.
	// See RHOAIENG-59726.
	if err := restoreGatewayNamespaceResources(resMap); err != nil {
		return nil, fmt.Errorf("restore gateway namespace for payload-processing: %w", err)
	}

	componentLabels := map[string]string{
		labels.ODH.Component(componentApi.ModelsAsServiceComponentName): labels.True,
		labels.K8SCommon.PartOf: componentApi.ModelsAsServiceComponentName,
	}
	if err := plugins.CreateSetLabelsPlugin(componentLabels).Transform(resMap); err != nil {
		return nil, fmt.Errorf("labels transform for maas-controller bundle: %w", err)
	}

	paramsEnvPath := filepath.Join(mi.Path, mi.ContextDir, BaseManifestsSourcePath, "params.env")
	if err := applyImageOverridesFromParams(resMap, paramsEnvPath); err != nil {
		return nil, fmt.Errorf("image override for maas-controller bundle: %w", err)
	}

	rendered := resMap.Resources()
	extra := make([]unstructured.Unstructured, 0, len(rendered)+1)
	for i := range rendered {
		m, err := rendered[i].Map()
		if err != nil {
			return nil, fmt.Errorf("maas-controller bundle resource map: %w", err)
		}
		m, err = normalizeUnstructuredObject(m)
		if err != nil {
			return nil, fmt.Errorf("normalize maas-controller bundle object: %w", err)
		}
		extra = append(extra, unstructured.Unstructured{Object: m})
	}

	paramsCM, err := maasParametersConfigMapFromParamsEnv(root, appNs, monitoringNs, componentLabels)
	if err != nil {
		return nil, fmt.Errorf("build maas-parameters ConfigMap from params.env: %w", err)
	}
	extra = append(extra, *paramsCM)

	extra = append(extra, payloadProcessingNetworkPolicy(componentLabels))

	out := make([]client.Object, len(extra))
	for i := range extra {
		out[i] = &extra[i]
	}

	return out, nil
}

// maasParametersConfigMapFromParamsEnv reads the already-updated params.env
// (Init → ApplyParams has already merged RELATED_IMAGE_* and extraParamsMap)
// and builds the maas-parameters ConfigMap that is deployed alongside
// maas-controller. This is the authoritative source of maas-parameters;
// the Tenant reconciler consumes it rather than regenerating it.
func maasParametersConfigMapFromParamsEnv(manifestsBasePath string, appNs string, monitoringNs string, componentLabels map[string]string) (*unstructured.Unstructured, error) {
	paramsFile := filepath.Join(manifestsBasePath, MaasManifestContextDir, BaseManifestsSourcePath, "params.env")
	paramsMap, err := parseParamsEnv(paramsFile)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", paramsFile, err)
	}

	// Override app-namespace with the resolved application namespace so that
	// RHOAI deployments (redhat-ods-applications) don't use the ODH default
	// hardcoded in params.env (opendatahub).
	paramsMap["app-namespace"] = appNs

	// Override monitoring-namespace with the resolved monitoring namespace so that
	// RHOAI deployments (redhat-ods-monitoring) don't use the ODH default
	// hardcoded in params.env (opendatahub).
	paramsMap["monitoring-namespace"] = monitoringNs

	data := make(map[string]any, len(paramsMap))
	for k, v := range paramsMap {
		data[k] = v
	}

	cm := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]any{
				"name":      "maas-parameters",
				"namespace": appNs,
				"labels":    toStringInterfaceMap(componentLabels),
			},
			"data": data,
		},
	}
	return cm, nil
}

// payloadProcessingNetworkPolicy returns a NetworkPolicy for the
// payload-processing pod in the gateway namespace. OCP 4.22 introduced a
// deny-all NetworkPolicy in openshift-ingress; without explicit rules the pod
// cannot reach the Kubernetes API server (egress) or receive ext_proc calls
// from the gateway (ingress).
func payloadProcessingNetworkPolicy(componentLabels map[string]string) unstructured.Unstructured {
	npLabels := make(map[string]any, len(componentLabels)+1)
	for k, v := range componentLabels {
		npLabels[k] = v
	}
	npLabels["app"] = "payload-processing"

	return unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "networking.k8s.io/v1",
			"kind":       "NetworkPolicy",
			"metadata": map[string]any{
				"name":      "payload-processing",
				"namespace": DefaultGatewayNamespace,
				"labels":    npLabels,
			},
			"spec": map[string]any{
				"podSelector": map[string]any{
					"matchLabels": map[string]any{
						"app": "payload-processing",
					},
				},
				"policyTypes": []any{"Ingress", "Egress"},
				"ingress": []any{
					map[string]any{
						"from": []any{
							map[string]any{
								"podSelector": map[string]any{
									"matchLabels": map[string]any{
										"gateway.networking.k8s.io/gateway-name": "data-science-gateway",
									},
								},
								"namespaceSelector": map[string]any{
									"matchLabels": map[string]any{
										"kubernetes.io/metadata.name": DefaultGatewayNamespace,
									},
								},
							},
						},
						"ports": []any{
							map[string]any{
								"protocol": "TCP",
								"port":     int64(9004),
							},
						},
					},
					map[string]any{
						"from": []any{
							map[string]any{
								"namespaceSelector": map[string]any{
									"matchLabels": map[string]any{
										"kubernetes.io/metadata.name": "openshift-monitoring",
									},
								},
							},
							map[string]any{
								"namespaceSelector": map[string]any{
									"matchLabels": map[string]any{
										"kubernetes.io/metadata.name": "openshift-user-workload-monitoring",
									},
								},
							},
						},
						"ports": []any{
							map[string]any{
								"protocol": "TCP",
								"port":     int64(9005),
							},
							map[string]any{
								"protocol": "TCP",
								"port":     int64(9090),
							},
						},
					},
				},
				"egress": []any{map[string]any{}},
			},
		},
	}
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

func toStringInterfaceMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func normalizeUnstructuredObject(obj map[string]any) (map[string]any, error) {
	b, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

type resourceKey struct {
	kind string
	name string
}

// gatewayNamespaceResources lists the specific resources that must remain in the
// gateway namespace after the blanket NamespaceApplierPlugin transform. Keyed by
// kind+name to avoid accidentally matching unrelated resources.
var gatewayNamespaceResources = map[resourceKey]bool{
	{kind: "Deployment", name: "payload-processing"}:                true,
	{kind: "Service", name: "payload-processing"}:                   true,
	{kind: "ServiceAccount", name: "payload-processing"}:            true,
	{kind: "ConfigMap", name: "payload-processing-plugins"}:         true,
	{kind: "NetworkPolicy", name: "payload-processing"}:             true,
	{kind: "ClusterRoleBinding", name: "payload-processing-reader"}: true,
}

// restoreGatewayNamespaceResources moves resources that belong in the gateway
// namespace back from the application namespace. The blanket NamespaceApplierPlugin
// moves everything to appNs, but payload-processing resources must stay in the
// gateway namespace for the EnvoyFilter to attach to the Gateway.
//
// For ClusterRoleBindings, the NamespaceApplierPlugin also rewrites
// subjects[].namespace; this function restores it to the gateway namespace
// so the RBAC binding matches the actual ServiceAccount location.
func restoreGatewayNamespaceResources(resMap resmap.ResMap) error {
	for _, res := range resMap.Resources() {
		k := resourceKey{kind: res.GetKind(), name: res.GetName()}
		if !gatewayNamespaceResources[k] {
			continue
		}
		if res.GetKind() == "ClusterRoleBinding" {
			if err := restoreCRBSubjectsNamespace(res, DefaultGatewayNamespace); err != nil {
				return fmt.Errorf("restore subjects namespace on ClusterRoleBinding %s: %w", res.GetName(), err)
			}
			continue
		}
		if err := res.SetNamespace(DefaultGatewayNamespace); err != nil {
			return fmt.Errorf("set namespace on %s %s: %w", res.GetKind(), res.GetName(), err)
		}
	}
	return nil
}

// restoreCRBSubjectsNamespace updates subjects[].namespace on a ClusterRoleBinding
// resource. SetNamespace only changes metadata.namespace which is meaningless for
// cluster-scoped resources; the NamespaceApplierPlugin rewrites subjects separately.
func restoreCRBSubjectsNamespace(res *resource.Resource, namespace string) error {
	m, err := res.Map()
	if err != nil {
		return err
	}
	subjects, ok := m["subjects"].([]any)
	if !ok {
		return nil
	}
	for _, s := range subjects {
		if subject, ok := s.(map[string]any); ok {
			subject["namespace"] = namespace
		}
	}
	node, err := kyaml.FromMap(m)
	if err != nil {
		return err
	}
	res.RNode = *node
	return nil
}

// deployImageParams maps the kustomize image name (as rendered by the images
// transformer in manager/kustomization.yaml) to the params.env key that
// Init → ApplyParams populates from RELATED_IMAGE_* env vars.
var deployImageParams = map[string]string{
	"quay.io/opendatahub/maas-controller": "maas-controller-image",
}

// applyImageOverridesFromParams reads the already-updated params.env and uses
// kustomize's ImageTagTransformerPlugin to replace default images in the
// rendered resources. Init → ApplyParams has already merged RELATED_IMAGE_*
// values into params.env, so this keeps params.env as the single source of
// truth -- consistent with how all other components handle image substitution.
func applyImageOverridesFromParams(m resmap.ResMap, paramsEnvPath string) error {
	params, err := parseParamsEnv(paramsEnvPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", paramsEnvPath, err)
	}

	for kustomizeName, paramKey := range deployImageParams {
		override := params[paramKey]
		if override == "" {
			continue
		}

		img := parseImageRef(kustomizeName, override)
		plugin := &builtins.ImageTagTransformerPlugin{ImageTag: img}
		if err := plugin.Transform(m); err != nil {
			return fmt.Errorf("applying image override %s=%s: %w", paramKey, override, err)
		}
	}
	return nil
}

// parseImageRef builds a kustomize Image from a full image reference.
// It handles both tag (repo:tag) and digest (repo@sha256:...) formats.
func parseImageRef(kustomizeName, ref string) types.Image {
	if idx := strings.LastIndex(ref, "@"); idx > 0 {
		return types.Image{
			Name:    kustomizeName,
			NewName: ref[:idx],
			Digest:  ref[idx+1:],
		}
	}
	if idx := strings.LastIndex(ref, ":"); idx > 0 {
		return types.Image{
			Name:    kustomizeName,
			NewName: ref[:idx],
			NewTag:  ref[idx+1:],
		}
	}
	return types.Image{
		Name:    kustomizeName,
		NewName: ref,
	}
}
