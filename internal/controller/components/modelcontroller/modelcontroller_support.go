package modelcontroller

import (
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = componentApi.ModelControllerComponentName

	ReadyConditionType = componentApi.ModelControllerKind + status.ReadySuffix

	// LegacyComponentName is the name of the component that is assigned to deployments
	// via Kustomize. Since a deployment selector is immutable, we can't upgrade existing
	// deployment to the new component name, so keep it around till we figure out a solution.
	LegacyComponentName = "odh-model-controller"
)

var (
	imageParamMap = map[string]string{
		"odh-model-controller":    "RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE",
		"caikit-standalone-image": "RELATED_IMAGE_ODH_CAIKIT_NLP_IMAGE",
		"ovms-image":              "RELATED_IMAGE_ODH_OPENVINO_MODEL_SERVER_IMAGE",
		"vllm-cuda-image":         "RELATED_IMAGE_RHAIIS_VLLM_CUDA_IMAGE",
		"vllm-cpu-image":          "RELATED_IMAGE_ODH_VLLM_CPU_IMAGE",
		"vllm-gaudi-image":        "RELATED_IMAGE_ODH_VLLM_GAUDI_IMAGE",
		"vllm-rocm-image":         "RELATED_IMAGE_RHAIIS_VLLM_ROCM_IMAGE",
		"vllm-spyre-x86-image":    "RELATED_IMAGE_RHAIIS_VLLM_AMD64_SPYRE_IMAGE",
		"vllm-spyre-s390x-image":  "RELATED_IMAGE_RHAIIS_VLLM_S390X_SPYRE_IMAGE",
		"guardrails-detector-huggingface-runtime-image": "RELATED_IMAGE_ODH_GUARDRAILS_DETECTOR_HUGGINGFACE_RUNTIME_IMAGE",
	}

	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
	}
)

func manifestsPath() types.ManifestInfo {
	return types.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: "base",
	}
}
