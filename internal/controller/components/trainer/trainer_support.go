package trainer

import (
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = componentApi.TrainerComponentName

	ReadyConditionType = componentApi.TrainerKind + status.ReadySuffix
)

var (
	imageParamMap = map[string]string{
		"odh-kubeflow-trainer-controller-image": "RELATED_IMAGE_ODH_KUBEFLOW_TRAINER_CONTROLLER_IMAGE",
	}

	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
	}
)

func manifestPath() types.ManifestInfo {
	return types.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: "rhoai",
	}
}
