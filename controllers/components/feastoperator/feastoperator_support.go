package feastoperator

import (
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = componentApi.FeastOperatorComponentName

	ReadyConditionType = conditionsv1.ConditionType(componentApi.FeastOperatorKind + status.ReadySuffix)

	// LegacyComponentName is the name of the component that is assigned to deployments
	// via Kustomize. Since a deployment selector is immutable, we can't upgrade existing
	// deployment to the new component name, so keep it around till we figure out a solution.
	LegacyComponentName = "feastoperator"

	ManifestsSourcePath = "overlays/odh"
)

var (
	imageParamMap = map[string]string{
		"RELATED_IMAGE_FEAST_OPERATOR": "RELATED_IMAGE_ODH_FEAST_OPERATOR_IMAGE",
		"RELATED_IMAGE_FEATURE_SERVER": "RELATED_IMAGE_ODH_FEAST_FEATURE_SERVER_IMAGE",
	}
)

func manifestPath() types.ManifestInfo {
	return types.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: ManifestsSourcePath,
	}
}
