package dashboard

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
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
)

var (
	SectionTitle = map[common.Platform]string{
		cluster.SelfManagedRhoai: "OpenShift Self Managed Services",
		cluster.ManagedRhoai:     "OpenShift Managed Services",
		cluster.OpenDataHub:      "OpenShift Open Data Hub",
	}

	BaseConsoleURL = map[common.Platform]string{
		cluster.SelfManagedRhoai: "https://rhods-dashboard-",
		cluster.ManagedRhoai:     "https://rhods-dashboard-",
		cluster.OpenDataHub:      "https://odh-dashboard-",
	}

	OverlaysSourcePaths = map[common.Platform]string{
		cluster.SelfManagedRhoai: "/rhoai/onprem",
		cluster.ManagedRhoai:     "/rhoai/addon",
		cluster.OpenDataHub:      "/odh",
	}

	ImagesMap = map[string]string{
		"odh-dashboard-image":     "RELATED_IMAGE_ODH_DASHBOARD_IMAGE",
		"model-registry-ui-image": "RELATED_IMAGE_ODH_MOD_ARCH_MODEL_REGISTRY_IMAGE",
		"oauth-proxy-image":       "RELATED_IMAGE_OSE_OAUTH_PROXY_IMAGE",
	}

	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
	}
)

// DefaultManifestInfo constructs a ManifestInfo configured for the component's default
// overlays path for the given platform.
// 
// The returned ManifestInfo sets the manifest Path to the default manifest path,
// the ContextDir to the component name, and the SourcePath to the platform-specific
// overlays source path.
func DefaultManifestInfo(p common.Platform) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: OverlaysSourcePaths[p],
	}
}

// BffManifestsPath provides the ManifestInfo for the Backend-for-Frontend (BFF) manifests.
// It sets Path to odhdeploy.DefaultManifestPath, ContextDir to ComponentName, and SourcePath to "modular-architecture".
func BffManifestsPath() odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: "modular-architecture",
	}
}

// ComputeKustomizeVariable builds Kustomize variables for the dashboard overlays for the given platform and DSC initialization spec.
// It returns a map with:
//   - "dashboard-url": concatenation of the platform's base console URL prefix, the DSC applications namespace, a dot, and the console route domain (i.e. "<baseConsoleURL><applicationsNamespace>.<consoleDomain>").
//   - "section-title": the platform-specific section title.
// An error is returned if the console route domain cannot be obtained.
func ComputeKustomizeVariable(ctx context.Context, cli client.Client, platform common.Platform, dscispec *dsciv1.DSCInitializationSpec) (map[string]string, error) {
	consoleLinkDomain, err := cluster.GetDomain(ctx, cli)
	if err != nil {
		return nil, fmt.Errorf("error getting console route URL %s : %w", consoleLinkDomain, err)
	}

	return map[string]string{
		"dashboard-url": BaseConsoleURL[platform] + dscispec.ApplicationsNamespace + "." + consoleLinkDomain,
		"section-title": SectionTitle[platform],
	}, nil
}

// ComputeComponentName returns the appropriate legacy component name based on the platform.
// Platforms whose release.Name equals cluster.SelfManagedRhoai or cluster.ManagedRhoai
// return LegacyComponentNameDownstream, while all others return LegacyComponentNameUpstream.
// This distinction exists because these specific platforms use legacy downstream vs upstream
// naming conventions. This is historical behavior that must be preserved - do not change
// ComputeComponentName determines the legacy component name to use for deployments.
// It returns the downstream legacy name for SelfManagedRhoai and ManagedRhoai releases,
// and the upstream legacy name for all other releases.
func ComputeComponentName() string {
	release := cluster.GetRelease()

	name := LegacyComponentNameUpstream
	if release.Name == cluster.SelfManagedRhoai || release.Name == cluster.ManagedRhoai {
		name = LegacyComponentNameDownstream
	}

	return name
}
