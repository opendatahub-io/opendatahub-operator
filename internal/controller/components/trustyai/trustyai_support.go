package trustyai

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = componentApi.TrustyAIComponentName

	ReadyConditionType = componentApi.TrustyAIKind + status.ReadySuffix

	// LegacyComponentName is the name of the component that is assigned to deployments
	// via Kustomize. Since a deployment selector is immutable, we can't upgrade existing
	// deployment to the new component name, so keep it around till we figure out a solution.
	LegacyComponentName = "trustyai"
)

var (
	imageParamMap = map[string]string{
		"trustyaiServiceImage":               "RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_IMAGE",
		"trustyaiOperatorImage":              "RELATED_IMAGE_ODH_TRUSTYAI_SERVICE_OPERATOR_IMAGE",
		"lmes-driver-image":                  "RELATED_IMAGE_ODH_TA_LMES_DRIVER_IMAGE",
		"lmes-pod-image":                     "RELATED_IMAGE_ODH_TA_LMES_JOB_IMAGE",
		"guardrails-orchestrator-image":      "RELATED_IMAGE_ODH_FMS_GUARDRAILS_ORCHESTRATOR_IMAGE",
		"guardrails-sidecar-gateway-image":   "RELATED_IMAGE_ODH_TRUSTYAI_VLLM_ORCHESTRATOR_GATEWAY_IMAGE",
		"guardrails-built-in-detector-image": "RELATED_IMAGE_ODH_BUILT_IN_DETECTOR_IMAGE",
		"ragas-provider-image":               "RELATED_IMAGE_ODH_TRUSTYAI_RAGAS_LLS_PROVIDER_DSP_IMAGE",
		"oauthProxyImage":                    "RELATED_IMAGE_OSE_OAUTH_PROXY_IMAGE",
		"kube-rbac-proxy":                    "RELATED_IMAGE_OSE_KUBE_RBAC_PROXY_IMAGE",
	}

	overlaysSourcePaths = map[common.Platform]string{
		cluster.SelfManagedRhoai: "/overlays/rhoai",
		cluster.ManagedRhoai:     "/overlays/rhoai",
		cluster.OpenDataHub:      "/overlays/odh",
	}

	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
	}
)

func manifestsPath(p common.Platform) types.ManifestInfo {
	return types.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: overlaysSourcePaths[p],
	}
}
