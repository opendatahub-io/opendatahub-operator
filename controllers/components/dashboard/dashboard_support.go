package dashboard

import (
	"context"
	"fmt"

	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName = componentApi.DashboardComponentName

	ReadyConditionType = conditionsv1.ConditionType(componentApi.DashboardKind + status.ReadySuffix)

	// Legacy component names are the name of the component that is assigned to deployments
	// via Kustomize. Since a deployment selector is immutable, we can't upgrade existing
	// deployment to the new component name, so keep it around till we figure out a solution.

	LegacyComponentNameUpstream   = "dashboard"
	LegacyComponentNameDownstream = "rhods-dashboard"
)

var (
	adminGroups = map[cluster.Platform]string{
		cluster.SelfManagedRhoai: "rhods-admins",
		cluster.ManagedRhoai:     "dedicated-admins",
		cluster.OpenDataHub:      "odh-admins",
		cluster.Unknown:          "odh-admins",
	}

	sectionTitle = map[cluster.Platform]string{
		cluster.SelfManagedRhoai: "OpenShift Self Managed Services",
		cluster.ManagedRhoai:     "OpenShift Managed Services",
		cluster.OpenDataHub:      "OpenShift Open Data Hub",
		cluster.Unknown:          "OpenShift Open Data Hub",
	}

	baseConsoleURL = map[cluster.Platform]string{
		cluster.SelfManagedRhoai: "https://rhods-dashboard-",
		cluster.ManagedRhoai:     "https://rhods-dashboard-",
		cluster.OpenDataHub:      "https://odh-dashboard-",
		cluster.Unknown:          "https://odh-dashboard-",
	}

	overlaysSourcePaths = map[cluster.Platform]string{
		cluster.SelfManagedRhoai: "/rhoai/onprem",
		cluster.ManagedRhoai:     "/rhoai/addon",
		cluster.OpenDataHub:      "/odh",
		cluster.Unknown:          "/odh",
	}

	serviceAccounts = map[cluster.Platform][]string{
		cluster.SelfManagedRhoai: {"rhods-dashboard"},
		cluster.ManagedRhoai:     {"rhods-dashboard"},
		cluster.OpenDataHub:      {"odh-dashboard"},
		cluster.Unknown:          {"odh-dashboard"},
	}

	imagesMap = map[string]string{
		"odh-dashboard-image": "RELATED_IMAGE_ODH_DASHBOARD_IMAGE",
	}
)

func defaultManifestInfo(p cluster.Platform) odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: overlaysSourcePaths[p],
	}
}

func computeKustomizeVariable(ctx context.Context, cli client.Client, platform cluster.Platform, dscispec *dsciv1.DSCInitializationSpec) (map[string]string, error) {
	consoleLinkDomain, err := cluster.GetDomain(ctx, cli)
	if err != nil {
		return nil, fmt.Errorf("error getting console route URL %s : %w", consoleLinkDomain, err)
	}

	return map[string]string{
		"admin_groups":  adminGroups[platform],
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

func GetAdminGroup() string {
	return adminGroups[cluster.GetRelease().Name]
}
