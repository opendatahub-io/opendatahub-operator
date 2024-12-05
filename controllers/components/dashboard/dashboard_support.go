package dashboard

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName           = "dashboard"
	ComponentNameUpstream   = ComponentName
	ComponentNameDownstream = "rhods-dashboard"
)

var (
	PathUpstream          = odhdeploy.DefaultManifestPath + "/" + ComponentNameUpstream + "/odh"
	PathDownstream        = odhdeploy.DefaultManifestPath + "/" + ComponentNameUpstream + "/rhoai"
	PathSelfDownstream    = PathDownstream + "/onprem"
	PathManagedDownstream = PathDownstream + "/addon"

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

	manifestPaths = map[cluster.Platform]string{
		cluster.SelfManagedRhoai: PathSelfDownstream,
		cluster.ManagedRhoai:     PathManagedDownstream,
		cluster.OpenDataHub:      PathUpstream,
		cluster.Unknown:          PathUpstream,
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
		Path:       manifestPaths[p],
		ContextDir: "",
		SourcePath: "",
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

	name := ComponentNameUpstream
	if release.Name == cluster.SelfManagedRhoai || release.Name == cluster.ManagedRhoai {
		name = ComponentNameDownstream
	}

	return name
}
