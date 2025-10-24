package dashboard

import (
	"context"
	"errors"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = componentApi.DashboardComponentName

	ReadyConditionType = componentApi.DashboardKind + status.ReadySuffix

	// Legacy component names are the name of the component that is assigned to deployments
	// via Kustomize. Since a deployment selector is immutable, we can't upgrade existing
	// deployment to the new component name, so keep it around till we figure out a solution.

	LegacyComponentNameUpstream   = "dashboard"
	LegacyComponentNameDownstream = "rhods-dashboard"

	// Dashboard path on the gateway.
	dashboardPath = "/"
)

var (
	SectionTitle = map[common.Platform]string{
		cluster.SelfManagedRhoai: "OpenShift Self Managed Services",
		cluster.ManagedRhoai:     "OpenShift Managed Services",
		cluster.OpenDataHub:      "OpenShift Open Data Hub",
	}

	overlaysSourcePaths = map[common.Platform]string{
		cluster.SelfManagedRhoai: "/rhoai/onprem",
		cluster.ManagedRhoai:     "/rhoai/addon",
		cluster.OpenDataHub:      "/odh",
	}

	ImagesMap = map[string]string{
		"odh-dashboard-image":     "RELATED_IMAGE_ODH_DASHBOARD_IMAGE",
		"model-registry-ui-image": "RELATED_IMAGE_ODH_MOD_ARCH_MODEL_REGISTRY_IMAGE",
		"gen-ai-ui-image":         "RELATED_IMAGE_ODH_MOD_ARCH_GEN_AI_IMAGE",
		"kube-rbac-proxy":         "RELATED_IMAGE_OSE_KUBE_RBAC_PROXY_IMAGE",
	}

	ConditionTypes = []string{
		status.ConditionDeploymentsAvailable,
	}
)

func DefaultManifestInfo(p common.Platform) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: OverlaysSourcePaths[p],
	}
}

func BffManifestsPath() odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: ModularArchitectureSourcePath,
	}
}

func computeKustomizeVariable(ctx context.Context, cli client.Client, platform common.Platform) (map[string]string, error) {
	gatewayDomain, err := gateway.GetGatewayDomain(ctx, cli)
	if err != nil {
		return nil, fmt.Errorf("error getting gateway domain: %w", err)
	}

	return map[string]string{
		"dashboard-url": fmt.Sprintf("https://%s%s", gatewayDomain, dashboardPath),
		"section-title": sectionTitle[platform],
	}, nil
}

// ComputeComponentNameWithRelease returns the appropriate legacy component name based on the provided release.
// Platforms whose release.Name equals cluster.SelfManagedRhoai or cluster.ManagedRhoai
// return LegacyComponentNameDownstream, while all others return LegacyComponentNameUpstream.
// This distinction exists because these specific platforms use legacy downstream vs upstream
// naming conventions. This is historical behavior that must be preserved - do not change
// return values as this maintains compatibility with existing deployments.
func ComputeComponentNameWithRelease(release common.Release) string {
	name := LegacyComponentNameUpstream
	if release.Name == cluster.SelfManagedRhoai || release.Name == cluster.ManagedRhoai {
		name = LegacyComponentNameDownstream
	}

	return name
}

// ComputeComponentName returns the appropriate legacy component name based on the platform.
// This function maintains backward compatibility by using the global release state.
func ComputeComponentName() string {
	return ComputeComponentNameWithRelease(cluster.GetRelease())
}
