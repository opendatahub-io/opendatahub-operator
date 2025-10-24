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
	ModularArchitectureSourcePath = "modular-architecture"

	// Error message for unsupported platforms.
	ErrUnsupportedPlatform = "unsupported platform: %s"

	// Dashboard path on the gateway.
	dashboardPath = "/"
)

var (
	// Private maps to reduce API surface and prevent direct access.
	sectionTitle = map[common.Platform]string{
		cluster.SelfManagedRhoai: "OpenShift Self Managed Services",
		cluster.ManagedRhoai:     "OpenShift Managed Services",
		cluster.OpenDataHub:      "OpenShift Open Data Hub",
	}

	baseConsoleURL = map[common.Platform]string{
		cluster.SelfManagedRhoai: "https://rhods-dashboard-",
		cluster.ManagedRhoai:     "https://rhods-dashboard-",
		cluster.OpenDataHub:      "https://odh-dashboard-",
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

// GetSectionTitle returns the section title for the given platform.
// Returns an error if the platform is not supported.
func GetSectionTitle(platform common.Platform) (string, error) {
	title, ok := sectionTitle[platform]
	if !ok {
		return "", fmt.Errorf(ErrUnsupportedPlatform, platform)
	}
	return title, nil
}

// GetBaseConsoleURL returns the base console URL for the given platform.
// Returns an error if the platform is not supported.
func GetBaseConsoleURL(platform common.Platform) (string, error) {
	url, ok := baseConsoleURL[platform]
	if !ok {
		return "", fmt.Errorf(ErrUnsupportedPlatform, platform)
	}
	return url, nil
}

// GetOverlaysSourcePath returns the overlays source path for the given platform.
// Returns an error if the platform is not supported.
func GetOverlaysSourcePath(platform common.Platform) (string, error) {
	path, ok := overlaysSourcePaths[platform]
	if !ok {
		return "", fmt.Errorf(ErrUnsupportedPlatform, platform)
	}
	return path, nil
}

func DefaultManifestInfo(p common.Platform) (odhtypes.ManifestInfo, error) {
	sourcePath, err := GetOverlaysSourcePath(p)
	if err != nil {
		return odhtypes.ManifestInfo{}, err
	}

	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: sourcePath,
	}, nil
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

	baseURL, err := GetBaseConsoleURL(platform)
	if err != nil {
		return nil, err
	}
	sectionTitle, err := GetSectionTitle(platform)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"dashboard-url": baseURL + gatewayDomain,
		"section-title": sectionTitle,
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
