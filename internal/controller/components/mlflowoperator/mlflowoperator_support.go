package mlflowoperator

import (
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = componentApi.MlFlowOperatorComponentName

	ReadyConditionType = componentApi.MlFlowOperatorKind + status.ReadySuffix
)

var (
	imageParamMap = map[string]string{
		// TODO: Add your MLflow operator image parameter mappings here
		// Format: "placeholder-in-manifest": "ENVIRONMENT_VARIABLE_NAME"
		//
		// Common MLflow images you might need:
		// "mlflow-server-image":        "RELATED_IMAGE_ODH_MLFLOW_SERVER_IMAGE",
		// "mlflow-tracking-server":     "RELATED_IMAGE_ODH_MLFLOW_TRACKING_SERVER_IMAGE",
		// "mlflow-operator-image":      "RELATED_IMAGE_ODH_MLFLOW_OPERATOR_IMAGE",
		// "postgresql-image":           "RELATED_IMAGE_ODH_MLFLOW_POSTGRES_IMAGE",
		// "mlflow-ui-image":            "RELATED_IMAGE_ODH_MLFLOW_UI_IMAGE",
		//
		// These placeholders should match what's in your params.env file in the manifests
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
