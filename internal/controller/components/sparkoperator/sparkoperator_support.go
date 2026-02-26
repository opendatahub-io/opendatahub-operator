package sparkoperator

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = componentApi.SparkOperatorComponentName

	ReadyConditionType = componentApi.SparkOperatorKind + status.ReadySuffix
)

var (
	ManifestsSourcePath = map[common.Platform]string{
		cluster.SelfManagedRhoai: "overlays/rhoai",
		cluster.ManagedRhoai:     "overlays/rhoai",
		cluster.OpenDataHub:      "overlays/odh",
	}

	// Image parameter mapping.
	// Maps variables in params.env files to RELATED_IMAGE_* environment variables
	// that are injected by the operator at runtime.
	imageParamMap = map[string]string{
		"SPARK_OPERATOR_CONTROLLER_IMAGE": "RELATED_IMAGE_SPARK_OPERATOR_IMAGE",
		"SPARK_OPERATOR_WEBHOOK_IMAGE":    "RELATED_IMAGE_SPARK_OPERATOR_IMAGE",
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
