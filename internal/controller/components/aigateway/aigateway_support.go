package aigateway

import (
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	ippManifestSourcePath = "overlays/odh"

	ReadyConditionType = componentApi.AIGatewayKind + status.ReadySuffix
)

var (
	ippImageParamMap = map[string]string{
		"payload-processing": "RELATED_IMAGE_ODH_AI_GATEWAY_PAYLOAD_PROCESSING_IMAGE",
	}

	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
	}
)

func ippManifestInfo(basePath string, sourcePath string) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       basePath,
		ContextDir: "ipp",
		SourcePath: sourcePath,
	}
}
