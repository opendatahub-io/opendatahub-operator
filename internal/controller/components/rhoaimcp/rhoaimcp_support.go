package rhoaimcp

import (
	"path"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	ComponentName      = componentApi.RhoaiMcpComponentName
	ReadyConditionType = componentApi.RhoaiMcpKind + status.ReadySuffix
)

var (
	ManifestsSourcePath = map[common.Platform]string{
		cluster.SelfManagedRhoai: "overlays/rhoai",
		cluster.OpenDataHub:      "overlays/odh",
	}

	imageParamMap = map[string]string{
		"RHOAI_MCP_IMAGE": "RELATED_IMAGE_ODH_RHOAI_MCP_IMAGE",
	}

	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
	}
)

func paramsPath(basePath string) string {
	return path.Join(basePath, ComponentName, "base")
}

func manifestPath(basePath string, p common.Platform) types.ManifestInfo {
	return types.ManifestInfo{
		Path:       basePath,
		ContextDir: ComponentName,
		SourcePath: ManifestsSourcePath[p],
	}
}
