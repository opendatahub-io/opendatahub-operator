package kserve

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

var (
	imageParamMap = map[string]string{
		"kserve-agent":                     "RELATED_IMAGE_ODH_KSERVE_AGENT_IMAGE",
		"kserve-controller":                "RELATED_IMAGE_ODH_KSERVE_CONTROLLER_IMAGE",
		"kserve-router":                    "RELATED_IMAGE_ODH_KSERVE_ROUTER_IMAGE",
		"kserve-storage-initializer":       "RELATED_IMAGE_ODH_KSERVE_STORAGE_INITIALIZER_IMAGE",
		"kserve-llm-d":                     "RELATED_IMAGE_RHAIIS_VLLM_CUDA_IMAGE",
		"kserve-llm-d-inference-scheduler": "RELATED_IMAGE_ODH_LLM_D_INFERENCE_SCHEDULER_IMAGE",
		"kserve-llm-d-routing-sidecar":     "RELATED_IMAGE_ODH_LLM_D_ROUTING_SIDECAR_IMAGE",
		"oauth-proxy":                      "RELATED_IMAGE_OSE_OAUTH_PROXY_IMAGE",
	}
)

func kserveManifestInfo(sourcePath string) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: componentName,
		SourcePath: sourcePath,
	}
}

func updateInferenceCM(inferenceServiceConfigMap *corev1.ConfigMap, isHeadless bool) error {
	deployData, err := getDeployConfig(inferenceServiceConfigMap)
	if err != nil {
		return err
	}

	// deploy
	// RawDeployment mode is the only supported mode
	deployData.DefaultDeploymentMode = "RawDeployment"
	deployDataBytes, err := json.MarshalIndent(deployData, "", " ")
	if err != nil {
		return fmt.Errorf("could not set values in configmap %s. %w", kserveConfigMapName, err)
	}
	inferenceServiceConfigMap.Data[DeployConfigName] = string(deployDataBytes)

	// ingress
	// RawDeployment mode is the only supported mode, so always disable ingress creation
	var ingressData map[string]interface{}
	if err = json.Unmarshal([]byte(inferenceServiceConfigMap.Data[IngressConfigKeyName]), &ingressData); err != nil {
		return fmt.Errorf("error retrieving value for key '%s' from configmap %s. %w", IngressConfigKeyName, kserveConfigMapName, err)
	}
	ingressData["disableIngressCreation"] = true
	ingressDataBytes, err := json.MarshalIndent(ingressData, "", " ")
	if err != nil {
		return fmt.Errorf("could not set values in configmap %s. %w", kserveConfigMapName, err)
	}
	inferenceServiceConfigMap.Data[IngressConfigKeyName] = string(ingressDataBytes)

	// service
	var serviceData map[string]interface{}
	if err := json.Unmarshal([]byte(inferenceServiceConfigMap.Data[ServiceConfigKeyName]), &serviceData); err != nil {
		return fmt.Errorf("error retrieving value for key '%s' from configmap %s. %w", ServiceConfigKeyName, kserveConfigMapName, err)
	}
	serviceData["serviceClusterIPNone"] = isHeadless
	serviceDataBytes, err := json.MarshalIndent(serviceData, "", " ")
	if err != nil {
		return fmt.Errorf("could not set values in configmap %s. %w", kserveConfigMapName, err)
	}
	inferenceServiceConfigMap.Data[ServiceConfigKeyName] = string(serviceDataBytes)
	return nil
}

func getIndexedResource(rs []unstructured.Unstructured, obj any, g schema.GroupVersionKind, name string) (int, error) {
	var idx = -1
	for i, r := range rs {
		if r.GroupVersionKind() == g && r.GetName() == name {
			idx = i
			break
		}
	}

	if idx == -1 {
		return -1, fmt.Errorf("could not find %T with name %v in resources list", obj, name)
	}

	err := runtime.DefaultUnstructuredConverter.FromUnstructured(rs[idx].Object, obj)
	if err != nil {
		return idx, fmt.Errorf("failed converting to %T from resource %s: %w", obj, resources.FormatObjectReference(&rs[idx]), err)
	}

	return idx, nil
}

func replaceResourceAtIndex(rs []unstructured.Unstructured, idx int, obj any) error {
	u, err := resources.ToUnstructured(obj)
	if err != nil {
		return err
	}

	rs[idx] = *u
	return nil
}

func hashConfigMap(cm *corev1.ConfigMap) (string, error) {
	u, err := resources.ToUnstructured(cm)
	if err != nil {
		return "", err
	}

	h, err := resources.Hash(u)
	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(h), nil
}
