package trainer

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	ComponentName = componentApi.TrainerComponentName

	ReadyConditionType = componentApi.TrainerKind + status.ReadySuffix

	jobSetOperatorCRName = "cluster"

	jobSetConditionDegraded                       = "Degraded"
	jobSetConditionTargetConfigControllerDegraded = "TargetConfigControllerDegraded"
	jobSetConditionStaticResourcesDegraded        = "JobSetOperatorStaticResourcesDegraded"
	jobSetConditionAvailable                      = "Available"
)

var (
	imageParamMap = map[string]string{
		"odh-kubeflow-trainer-controller-image":           "RELATED_IMAGE_ODH_TRAINER_IMAGE",
		"odh-th-torch-cuda-py312-image":                   "RELATED_IMAGE_ODH_TH_TORCH_CUDA_PY312_IMAGE",
		"odh-th-torch-rocm-py312-image":                   "RELATED_IMAGE_ODH_TH_TORCH_ROCM_PY312_IMAGE",
		"odh-th-torch-cpu-py312-image":                    "RELATED_IMAGE_ODH_TH_TORCH_CPU_PY312_IMAGE",
		"odh-training-universal-workbench-image-cuda-3-5": "RELATED_IMAGE_ODH_TH_TORCH_CUDA_PY312_IMAGE",
		"odh-training-universal-workbench-image-rocm-3-5": "RELATED_IMAGE_ODH_TH_TORCH_ROCM_PY312_IMAGE",
		"odh-training-universal-workbench-image-cpu-3-5":  "RELATED_IMAGE_ODH_TH_TORCH_CPU_PY312_IMAGE",
	}

	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
		status.ConditionDependenciesAvailable,
	}
)

func manifestPath(basePath string) types.ManifestInfo {
	return types.ManifestInfo{
		Path:       basePath,
		ContextDir: ComponentName,
		SourcePath: "rhoai",
	}
}

// jobSetConditionFilter monitors JobSet operator conditions for degraded state.
func jobSetConditionFilter(condType, condStatus string) bool {
	switch condType {
	case jobSetConditionDegraded, jobSetConditionTargetConfigControllerDegraded, jobSetConditionStaticResourcesDegraded:
		return condStatus == string(metav1.ConditionTrue)
	case jobSetConditionAvailable:
		return condStatus == string(metav1.ConditionFalse)
	default:
		return false
	}
}
