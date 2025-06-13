package codeflare

import (
	"path"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = componentApi.CodeFlareComponentName

	ReadyConditionType = componentApi.CodeFlareKind + status.ReadySuffix
)

var (
	paramsPath = path.Join(odhdeploy.DefaultManifestPath, ComponentName, "manager")

	imageParamMap = map[string]string{
		"codeflare-operator-controller-image": "RELATED_IMAGE_ODH_CODEFLARE_OPERATOR_IMAGE",
	}

	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
	}
)

func manifestsPath() odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: "default",
	}
}
