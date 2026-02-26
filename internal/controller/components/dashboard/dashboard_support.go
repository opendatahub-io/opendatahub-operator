package dashboard

import (
	"errors"
	"fmt"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
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

	overlaysSourcePaths = map[common.Platform]string{
		cluster.SelfManagedRhoai: "/rhoai/onprem",
		cluster.ManagedRhoai:     "/rhoai/addon",
		cluster.OpenDataHub:      "/odh",
	}

	imagesMap = map[string]string{
		"odh-dashboard-image":     "RELATED_IMAGE_ODH_DASHBOARD_IMAGE",
		"model-registry-ui-image": "RELATED_IMAGE_ODH_MOD_ARCH_MODEL_REGISTRY_IMAGE",
		"gen-ai-ui-image":         "RELATED_IMAGE_ODH_MOD_ARCH_GEN_AI_IMAGE",
		"maas-ui-image":           "RELATED_IMAGE_ODH_MOD_ARCH_MAAS_IMAGE",
		"kube-rbac-proxy":         "RELATED_IMAGE_OSE_KUBE_RBAC_PROXY_IMAGE",
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

func bffManifestsPath() odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: "modular-architecture",
	}
}

func observabilityManifestInfo() odhtypes.ManifestInfo {
	return odhtypes.ManifestInfo{
		Path:       odhdeploy.DefaultManifestPath,
		ContextDir: ComponentName,
		SourcePath: "observability",
	}
}

func computeKustomizeVariable(rr *odhtypes.ReconciliationRequest, platform common.Platform) (map[string]string, error) {
	dashboard, ok := rr.Instance.(*componentApi.Dashboard)
	if !ok {
		return nil, errors.New("instance is not a Dashboard")
	}

	// Use domain from Dashboard.Spec.Gateway.Domain (synced from GatewayConfig.Status.Domain by DSC controller)
	var domain string
	if dashboard.Spec.Gateway != nil {
		domain = dashboard.Spec.Gateway.Domain
	}
	if domain == "" {
		return nil, errors.New(
			"gateway domain is missing for Dashboard; the Data Science Gateway may not be ready yet—check that " +
				"GatewayConfig exists and its status reports a domain")
	}

	return map[string]string{
		"dashboard-url":  fmt.Sprintf("https://%s/", domain),
		"section-title":  sectionTitle[platform],
		"gateway-domain": domain,
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
