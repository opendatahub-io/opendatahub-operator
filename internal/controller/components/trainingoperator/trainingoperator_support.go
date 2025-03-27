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

	// LegacyComponentName is the name of the component that is assigned to deployments
	// via Kustomize. Since a deployment selector is immutable, we can't upgrade existing
	// deployment to the new component name, so keep it around till we figure out a solution.
	LegacyComponentName = "trainingoperator"
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
