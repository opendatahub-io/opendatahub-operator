package mlflowoperator

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = componentApi.MLflowOperatorComponentName

	ReadyConditionType = componentApi.MLflowOperatorKind + status.ReadySuffix
)

var (
	sectionTitle = map[common.Platform]string{
		cluster.SelfManagedRhoai: "OpenShift Self Managed Services",
		cluster.OpenDataHub:      "OpenShift Open Data Hub",
	}

	ManifestsSourcePath = map[common.Platform]string{
		cluster.SelfManagedRhoai: "overlays/rhoai",
		cluster.OpenDataHub:      "overlays/odh",
	}

	imageParamMap = map[string]string{
		"MLFLOW_IMAGE":          "RELATED_IMAGE_ODH_MLFLOW_IMAGE",
		"MLFLOW_OPERATOR_IMAGE": "RELATED_IMAGE_ODH_MLFLOW_OPERATOR_IMAGE",
		"KUBE_AUTH_PROXY_IMAGE": "RELATED_IMAGE_OSE_KUBE_RBAC_PROXY_IMAGE",
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

func ComputeKustomizeVariable(ctx context.Context, cli client.Client, platform common.Platform) (map[string]string, error) {
	// Get the gateway domain directly from Gateway CR
	consoleLinkDomain, err := gateway.GetGatewayDomain(ctx, cli)
	if err != nil {
		return nil, fmt.Errorf("error getting gateway domain: %w", err)
	}

	return map[string]string{
		"mlflow-url":    fmt.Sprintf("https://%s/", consoleLinkDomain),
		"section-title": sectionTitle[platform],
	}, nil
}
