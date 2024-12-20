package codeflare

import (
	"path"

	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = componentApi.CodeFlareComponentName

	ReadyConditionType = conditionsv1.ConditionType(componentApi.CodeFlareKind + status.ReadySuffix)

	// LegacyComponentName is the name of the component that is assigned to deployments
	// via Kustomize. Since a deployment selector is immutable, we can't upgrade existing
	// deployment to the new component name, so keep it around till we figure out a solution.
	LegacyComponentName = "codeflare"
)

var (
	paramsPath = path.Join(odhdeploy.DefaultManifestPath, ComponentName, "manager")

	imageParamMap = map[string]string{
		"codeflare-operator-controller-image": "RELATED_IMAGE_ODH_CODEFLARE_OPERATOR_IMAGE",
	}
)

func manifestsPath() odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: "default",
	}
}
