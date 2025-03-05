package dashboard

import (
	"context"
	"fmt"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhdeploy "github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	adminGroups = map[common.Platform]string{
		cluster.SelfManagedRhoai: "rhods-admins",
		cluster.ManagedRhoai:     "dedicated-admins",
		cluster.OpenDataHub:      "odh-admins",
		cluster.Unknown:          "odh-admins",
	}

	sectionTitle = map[common.Platform]string{
		cluster.SelfManagedRhoai: "OpenShift Self Managed Services",
		cluster.ManagedRhoai:     "OpenShift Managed Services",
		cluster.OpenDataHub:      "OpenShift Open Data Hub",
		cluster.Unknown:          "OpenShift Open Data Hub",
	}

	baseConsoleURL = map[common.Platform]string{
		cluster.SelfManagedRhoai: "https://rhods-dashboard-",
		cluster.ManagedRhoai:     "https://rhods-dashboard-",
		cluster.OpenDataHub:      "https://odh-dashboard-",
		cluster.Unknown:          "https://odh-dashboard-",
	}

	overlaysSourcePaths = map[common.Platform]string{
		cluster.SelfManagedRhoai: "/rhoai/onprem",
		cluster.ManagedRhoai:     "/rhoai/addon",
		cluster.OpenDataHub:      "/odh",
		cluster.Unknown:          "/odh",
	}

	imagesMap = map[string]string{
		"odh-dashboard-image": "RELATED_IMAGE_ODH_DASHBOARD_IMAGE",
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

// TODO: to be removed: https://issues.redhat.com/browse/RHOAIENG-21080
func updateSpecFields(obj *unstructured.Unstructured, updates map[string][]any) (bool, error) {
	updated := false

	for field, newData := range updates {
		existingField, exists, err := unstructured.NestedSlice(obj.Object, "spec", field)
		if err != nil {
			return false, fmt.Errorf("failed to get field '%s': %w", field, err)
		}

		if !exists || len(existingField) == 0 {
			if err := unstructured.SetNestedSlice(obj.Object, newData, "spec", field); err != nil {
				return false, fmt.Errorf("failed to set field '%s': %w", field, err)
			}
			updated = true
		}
	}

	return updated, nil
}

// TODO: to be removed: https://issues.redhat.com/browse/RHOAIENG-21080
func getNotebookSizesData() []any {
	return []any{
		map[string]any{
			"name": "Small",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "1",
					"memory": "4Gi",
				},
				"limits": map[string]any{
					"cpu":    "3",
					"memory": "8Gi",
				},
			},
		},
		map[string]any{
			"name": "Small",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "1",
					"memory": "8Gi",
				},
				"limits": map[string]any{
					"cpu":    "2",
					"memory": "8Gi",
				},
			},
		},
		map[string]any{
			"name": "Medium",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "3",
					"memory": "24Gi",
				},
				"limits": map[string]any{
					"cpu":    "6",
					"memory": "24Gi",
				},
			},
		},
		map[string]any{
			"name": "Large",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "7",
					"memory": "56Gi",
				},
				"limits": map[string]any{
					"cpu":    "14",
					"memory": "56Gi",
				},
			},
		},
		map[string]any{
			"name": "X Large",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "15",
					"memory": "120Gi",
				},
				"limits": map[string]any{
					"cpu":    "30",
					"memory": "120Gi",
				},
			},
		},
	}
}

// TODO: to be removed: https://issues.redhat.com/browse/RHOAIENG-21080
func getModelServerSizeData() []any {
	return []any{
		map[string]any{
			"name": "Small",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "1",
					"memory": "4Gi",
				},
				"limits": map[string]any{
					"cpu":    "2",
					"memory": "8Gi",
				},
			},
		},
		map[string]any{
			"name": "Medium",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "4",
					"memory": "8Gi",
				},
				"limits": map[string]any{
					"cpu":    "8",
					"memory": "10Gi",
				},
			},
		},
		map[string]any{
			"name": "Large",
			"resources": map[string]any{
				"requests": map[string]any{
					"cpu":    "6",
					"memory": "16Gi",
				},
				"limits": map[string]any{
					"cpu":    "10",
					"memory": "20Gi",
				},
			},
		},
		map[string]any{
			"name": "Custom",
			"resources": map[string]any{
				"requests": map[string]any{},
				"limits":   map[string]any{},
			},
		},
	}
}
