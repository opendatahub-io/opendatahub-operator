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

	imagesMap = map[string]string{
		"odh-dashboard-image":     "RELATED_IMAGE_ODH_DASHBOARD_IMAGE",
		"model-registry-ui-image": "RELATED_IMAGE_ODH_MOD_ARCH_MODEL_REGISTRY_IMAGE",
	}

	conditionTypes = []string{
		status.ConditionDeploymentsAvailable,
	}
)

func defaultManifestInfo(p common.Platform) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: overlaysSourcePaths[p],
	}
}

func computeKustomizeVariable(ctx context.Context, cli client.Client, platform common.Platform, dscispec *dsciv1.DSCInitializationSpec) (map[string]string, error) {
	consoleLinkDomain, err := cluster.GetDomain(ctx, cli)
	if err != nil {
		return nil, fmt.Errorf("error getting console route URL %s : %w", consoleLinkDomain, err)
	}

	return map[string]string{
		"dashboard-url": baseConsoleURL[platform] + dscispec.ApplicationsNamespace + "." + consoleLinkDomain,
		"section-title": sectionTitle[platform],
	}, nil
}

func computeComponentName() string {
	release := cluster.GetRelease()

	name := LegacyComponentNameUpstream
	if release.Name == cluster.SelfManagedRhoai || release.Name == cluster.ManagedRhoai {
		name = LegacyComponentNameDownstream
	}

	return name
}
