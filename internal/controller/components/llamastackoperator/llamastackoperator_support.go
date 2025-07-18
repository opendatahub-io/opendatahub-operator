package llamastackoperator

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = componentApi.LlamaStackOperatorComponentName

	ReadyConditionType = componentApi.LlamaStackOperatorKind + status.ReadySuffix
)

var (
	ManifestsSourcePath = map[common.Platform]string{
		cluster.SelfManagedRhoai: "overlays/rhoai",
		cluster.ManagedRhoai:     "overlays/rhoai",
		cluster.OpenDataHub:      "overlays/odh",
	}

	// TODO: double check if downsteam is using this as placeholder.
	imageParamMap = map[string]string{
		"RELATED_IMAGE_ODH_LLAMASTACK_OPERATOR": "RELATED_IMAGE_ODH_LLAMA_STACK_K8S_OPERATOR_IMAGE",
		"RELATED_IMAGE_RH_DISTRIBUTION":         "RELATED_IMAGE_RH_DISTRIBUTION",
	}

	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
	}
)

func manifestPath(p common.Platform) types.ManifestInfo {
	return types.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: ManifestsSourcePath[p],
	}
}
