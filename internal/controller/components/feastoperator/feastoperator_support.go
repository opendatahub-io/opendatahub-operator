package feastoperator

import (
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = componentApi.FeastOperatorComponentName

	ReadyConditionType = componentApi.FeastOperatorKind + status.ReadySuffix

	ManifestsSourcePath = "overlays/odh"
)

var (
	imageParamMap = map[string]string{
		"RELATED_IMAGE_FEAST_OPERATOR": "RELATED_IMAGE_ODH_FEAST_OPERATOR_IMAGE",
		"RELATED_IMAGE_FEATURE_SERVER": "RELATED_IMAGE_ODH_FEATURE_SERVER_IMAGE",
	}

	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
	}
)

func manifestPath() types.ManifestInfo {
	return types.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: ManifestsSourcePath,
	}
}
