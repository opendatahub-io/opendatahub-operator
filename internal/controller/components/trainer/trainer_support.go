package trainer

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = componentApi.TrainerComponentName

	ReadyConditionType = componentApi.TrainerKind + status.ReadySuffix

	jobSetConditionDegraded                       = "Degraded"
	jobSetConditionTargetConfigControllerDegraded = "TargetConfigControllerDegraded"
	jobSetConditionStaticResourcesDegraded        = "JobSetOperatorStaticResourcesDegraded"
	jobSetConditionAvailable                      = "Available"
)

var (
	imageParamMap = map[string]string{
		"odh-kubeflow-trainer-controller-image":    "RELATED_IMAGE_ODH_TRAINER_IMAGE",
		"odh-training-cuda128-torch28-py312-image": "RELATED_IMAGE_ODH_TRAINING_CUDA128_TORCH28_PY312_IMAGE",
		"odh-training-rocm64-torch28-py312-image":  "RELATED_IMAGE_ODH_TRAINING_ROCM64_TORCH28_PY312_IMAGE",
	}

	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
		status.ConditionDependenciesAvailable,
	}
)

func manifestPath() types.ManifestInfo {
	return types.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
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
