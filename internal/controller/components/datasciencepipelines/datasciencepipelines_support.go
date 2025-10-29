package datasciencepipelines

import (
	"path"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ArgoWorkflowCRD = "workflows.argoproj.io"
	ComponentName   = componentApi.DataSciencePipelinesComponentName

	// ReadyConditionType is the condition type for AIPipelines in v2 (storage version).
	// The conversion webhook will translate this to DataSciencePipelinesReady for v1 users.
	ReadyConditionType = componentApi.AIPipelinesKind + status.ReadySuffix

	// LegacyComponentName is the name of the component that is assigned to deployments
	// via Kustomize. Since a deployment selector is immutable, we can't upgrade existing
	// deployment to the new component name, so keep it around till we figure out a solution.
	LegacyComponentName = "data-science-pipelines-operator"

	platformVersionParamsKey          = "PLATFORMVERSION"
	fipsEnabledParamsKey              = "FIPSENABLED"
	argoWorkflowsControllersParamsKey = "ARGOWORKFLOWSCONTROLLERS"
)

var (
	ErrArgoWorkflowAPINotOwned = odherrors.NewStopError(status.DataSciencePipelinesDoesntOwnArgoCRDMessage)
	ErrArgoWorkflowCRDMissing  = odherrors.NewStopError(status.DataSciencePipelinesArgoWorkflowsCRDMissingMessage)
)

var (
	imageParamMap = map[string]string{
		"IMAGES_DSPO":                    "RELATED_IMAGE_ODH_DATA_SCIENCE_PIPELINES_OPERATOR_CONTROLLER_IMAGE",
		"IMAGES_APISERVER":               "RELATED_IMAGE_ODH_ML_PIPELINES_API_SERVER_V2_IMAGE",
		"IMAGES_PERSISTENCEAGENT":        "RELATED_IMAGE_ODH_ML_PIPELINES_PERSISTENCEAGENT_V2_IMAGE",
		"IMAGES_SCHEDULEDWORKFLOW":       "RELATED_IMAGE_ODH_ML_PIPELINES_SCHEDULEDWORKFLOW_V2_IMAGE",
		"IMAGES_ARGO_EXEC":               "RELATED_IMAGE_ODH_DATA_SCIENCE_PIPELINES_ARGO_ARGOEXEC_IMAGE",
		"IMAGES_ARGO_WORKFLOWCONTROLLER": "RELATED_IMAGE_ODH_DATA_SCIENCE_PIPELINES_ARGO_WORKFLOWCONTROLLER_IMAGE",
		"IMAGES_DRIVER":                  "RELATED_IMAGE_ODH_ML_PIPELINES_DRIVER_IMAGE",
		"IMAGES_LAUNCHER":                "RELATED_IMAGE_ODH_ML_PIPELINES_LAUNCHER_IMAGE",
		"IMAGES_PIPELINESRUNTIMEGENERIC": "RELATED_IMAGE_ODH_ML_PIPELINES_RUNTIME_GENERIC_IMAGE",
		"IMAGES_MARIADB":                 "RELATED_IMAGE_DSP_MARIADB_IMAGE",
		"IMAGES_OAUTHPROXY":              "RELATED_IMAGE_OSE_OAUTH_PROXY_IMAGE",
		"IMAGES_TOOLBOX":                 "RELATED_IMAGE_DSP_TOOLBOX_IMAGE",
		"IMAGES_RHELAI":                  "RELATED_IMAGE_DSP_INSTRUCTLAB_NVIDIA_IMAGE",
		"kube-rbac-proxy":                "RELATED_IMAGE_OSE_KUBE_RBAC_PROXY_IMAGE",
	}

	overlaysSourcePaths = map[common.Platform]string{
		cluster.SelfManagedRhoai: "overlays/rhoai",
		cluster.ManagedRhoai:     "overlays/rhoai",
		cluster.OpenDataHub:      "overlays/odh",
	}

	conditionTypes = []string{
		status.ConditionArgoWorkflowAvailable,
		status.ConditionDeploymentsAvailable,
	}

	paramsPath = path.Join(odhdeploy.DefaultManifestPath, ComponentName, "base")
)

func manifestPath(p common.Platform) types.ManifestInfo {
	return types.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: overlaysSourcePaths[p],
	}
}
