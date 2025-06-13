package trainingoperator

import (
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = componentApi.TrainingOperatorComponentName

	ReadyConditionType = componentApi.TrainingOperatorKind + status.ReadySuffix
)

var (
	imageParamMap = map[string]string{
		"odh-training-operator-controller-image": "RELATED_IMAGE_ODH_TRAINING_OPERATOR_IMAGE",
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
