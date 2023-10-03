// Package dashboard provides utility functions to config Open Data Hub Dashboard: A web dashboard that displays
// installed Open Data Hub components with easy access to component UIs and documentation
package dashboard

import (
	"fmt"
	"path/filepath"

	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ComponentName          = "odh-dashboard"
	ComponentNameSupported = "rhods-dashboard"
	Path                   = deploy.DefaultManifestPath + "/" + ComponentName + "/base"
	PathSupported          = deploy.DefaultManifestPath + "/" + ComponentName + "/overlays/rhods"
	PathISVSM              = deploy.DefaultManifestPath + "/" + ComponentName + "/apps/apps-onprem"
	PathISVAddOn           = deploy.DefaultManifestPath + "/" + ComponentName + "/apps/apps-addon"
	PathOVMS               = deploy.DefaultManifestPath + "/" + ComponentName + "/modelserving"
	PathODHDashboardConfig = deploy.DefaultManifestPath + "/" + ComponentName + "/odhdashboardconfig"
	PathConsoleLink        = deploy.DefaultManifestPath + "/" + ComponentName + "/consolelink"
	PathCRDs               = deploy.DefaultManifestPath + "/" + ComponentName + "/crd"
	NameConsoleLink        = "console"
	NamespaceConsoleLink   = "openshift-console"
	PathAnaconda           = deploy.DefaultManifestPath + "/partners/anaconda/base/"
)

type Dashboard struct {
	components.Component `json:""`
}

func (d *Dashboard) OverrideManifests(platform string) error {
	// If devflags are set, update default manifests path
	if len(d.DevFlags.Manifests) != 0 {
		manifestConfig := d.DevFlags.Manifests[0]
		if err := deploy.DownloadManifests(ComponentName, manifestConfig); err != nil {
			return err
		}
		// If overlay is defined, update paths
		if platform == string(deploy.ManagedRhods) || platform == string(deploy.SelfManagedRhods) {
			defaultKustomizePath := "overlays/rhods"
			if manifestConfig.SourcePath != "" {
				defaultKustomizePath = manifestConfig.SourcePath
			}
			PathSupported = filepath.Join(deploy.DefaultManifestPath, ComponentName, defaultKustomizePath)
		} else {
			defaultKustomizePath := "base"
			if manifestConfig.SourcePath != "" {
				defaultKustomizePath = manifestConfig.SourcePath
			}
			Path = filepath.Join(deploy.DefaultManifestPath, ComponentName, defaultKustomizePath)
		}
	}
	return nil
}

func (d *Dashboard) GetComponentName() string {
	return ComponentName
}

// Verifies that Dashboard implements ComponentInterface.
var _ components.ComponentInterface = (*Dashboard)(nil)

func (d *Dashboard) ReconcileComponent(cli client.Client, owner metav1.Object, dscispec *dsciv1.DSCInitializationSpec) error {
	var imageParamMap = map[string]string{
		"odh-dashboard-image": "RELATED_IMAGE_ODH_DASHBOARD_IMAGE",
	}
	enabled := d.GetManagementState() == operatorv1.Managed
	monitoringEnabled := dscispec.Monitoring.ManagementState == operatorv1.Managed
	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}

	// Update Default rolebinding
	if enabled {
		// Download manifests and update paths
		if err = d.OverrideManifests(string(platform)); err != nil {
			return err
		}
		if platform == deploy.OpenDataHub || platform == "" {
			err := common.UpdatePodSecurityRolebinding(cli, []string{ComponentName}, dscispec.ApplicationsNamespace)
			if err != nil {
				return err
			}
			// Deploy CRDs
			if err := d.deployCRDsForPlatform(cli, owner, dscispec.ApplicationsNamespace, ComponentName, enabled); err != nil {
				return fmt.Errorf("failed to deploy %s crds %s: %v", ComponentName, PathCRDs, err)
			}
			// Deploy odh-dashboard manifests
			err = deploy.DeployManifestsFromPath(cli, owner, Path, dscispec.ApplicationsNamespace, ComponentName, enabled)
			if err != nil {
				return err
			}
		} else if platform == deploy.SelfManagedRhods || platform == deploy.ManagedRhods {
			// Update rolebinding
			err := common.UpdatePodSecurityRolebinding(cli, []string{ComponentNameSupported}, dscispec.ApplicationsNamespace)
			if err != nil {
				return err
			}
			// Deploy CRDs
			if err := d.deployCRDsForPlatform(cli, owner, dscispec.ApplicationsNamespace, ComponentNameSupported, enabled); err != nil {
				return fmt.Errorf("failed to deploy %s crds %s: %v", ComponentNameSupported, PathCRDs, err)
			}
			// Apply RHODS specific configs
			if err := d.applyRhodsSpecificConfigs(cli, owner, dscispec.ApplicationsNamespace, platform, enabled); err != nil {
				return err
			}

			// Update image parameters (ODH does not use this solution, only downstream)
			if dscispec.DevFlags.ManifestsUri == "" && len(d.DevFlags.Manifests) == 0 {
				if err := deploy.ApplyParams(PathSupported, d.SetImageParamsMap(imageParamMap), false); err != nil {
					return err
				}
			}
			// Apply authentication overlay
			err = deploy.DeployManifestsFromPath(cli, owner, PathSupported, dscispec.ApplicationsNamespace, ComponentNameSupported, enabled)
			if err != nil {
				return err
			}
		}
	}

	// ISV handling
	switch platform {
	case deploy.SelfManagedRhods:
		// ConsoleLink handling
		sectionTitle := "OpenShift Self Managed Services"
		isvPath := PathISVSM
		if err := d.deployConsoleLink(cli, owner, dscispec.ApplicationsNamespace, ComponentNameSupported, sectionTitle, enabled); err != nil {
			return err
		}
		// ISV
		if err := d.deployISVManifests(cli, owner, dscispec.ApplicationsNamespace, ComponentNameSupported, isvPath, enabled); err != nil {
			return err
		}
		return nil
	case deploy.ManagedRhods:
		// ConsoleLink handling
		sectionTitle := "OpenShift Managed Services"
		isvPath := PathISVAddOn
		if err := d.deployConsoleLink(cli, owner, dscispec.ApplicationsNamespace, ComponentNameSupported, sectionTitle, enabled); err != nil {
			return err
		}
		// ISV
		if err := d.deployISVManifests(cli, owner, dscispec.ApplicationsNamespace, ComponentNameSupported, isvPath, enabled); err != nil {
			return err
		}
		// CloudService Monitoring handling
		if err := d.UpdatePrometheusConfig(cli, enabled && monitoringEnabled, ComponentNameSupported); err != nil {
			return err
		}
		if err = deploy.DeployManifestsFromPath(cli, owner,
			filepath.Join(deploy.DefaultManifestPath, "monitoring", "prometheus", "apps"),
			dscispec.Monitoring.Namespace,
			ComponentName+"prometheus", true); err != nil {
			return err
		}
		return nil
	default:
		return nil
	}
}

func (d *Dashboard) DeepCopyInto(target *Dashboard) {
	*target = *d
	target.Component = d.Component
}

func (d *Dashboard) deployCRDsForPlatform(cli client.Client, owner metav1.Object, namespace string, componentName string, enabled bool) error {
	return deploy.DeployManifestsFromPath(cli, owner, PathCRDs, namespace, componentName, enabled)
}

func (d *Dashboard) applyRhodsSpecificConfigs(cli client.Client, owner metav1.Object, namespace string, platform deploy.Platform, enabled bool) error {
	dashboardConfig := filepath.Join(PathODHDashboardConfig, "odhdashboardconfig.yaml")
	adminGroups := map[deploy.Platform]string{
		deploy.SelfManagedRhods: "rhods-admins",
		deploy.ManagedRhods:     "dedicated-admins",
	}[platform]

	if err := common.ReplaceStringsInFile(dashboardConfig, map[string]string{"<admin_groups>": adminGroups}); err != nil {
		return err
	}

	if err := deploy.DeployManifestsFromPath(cli, owner, PathODHDashboardConfig, namespace, ComponentNameSupported, enabled); err != nil {
		return fmt.Errorf("failed to set dashboard config from %s: %w", PathODHDashboardConfig, err)
	}

	if err := deploy.DeployManifestsFromPath(cli, owner, PathOVMS, namespace, ComponentNameSupported, enabled); err != nil {
		return fmt.Errorf("failed to set dashboard OVMS from %s: %w", PathOVMS, err)
	}

	if err := common.CreateSecret(cli, "anaconda-ce-access", namespace); err != nil {
		return fmt.Errorf("failed to create access-secret for anaconda: %w", err)
	}

	return deploy.DeployManifestsFromPath(cli, owner, PathAnaconda, namespace, ComponentNameSupported, enabled)
}

func (d *Dashboard) deployISVManifests(cli client.Client, owner metav1.Object, namespace string, componentName string, path string, enabled bool) error {
	if err := deploy.DeployManifestsFromPath(cli, owner, path, namespace, componentName, enabled); err != nil {
		return fmt.Errorf("failed to set dashboard ISV from %s: %w", path, err)
	}

	return nil
}

func (d *Dashboard) deployConsoleLink(cli client.Client, owner metav1.Object, namespace string, componentName string, sectionTitle string, enabled bool) error {
	consolelinkDomain, err := common.GetDomain(cli, NameConsoleLink, NamespaceConsoleLink)
	if err != nil {
		return fmt.Errorf("error getting %s route from %s: %w", NameConsoleLink, NamespaceConsoleLink, err)
	}

	pathConsoleLink := filepath.Join(PathConsoleLink, "consolelink.yaml")
	err = common.ReplaceStringsInFile(pathConsoleLink, map[string]string{
		"<rhods-dashboard-url>": "https://rhods-dashboard-" + namespace + "." + consolelinkDomain,
		"<section-title>":       sectionTitle,
	})
	if err != nil {
		return fmt.Errorf("error replacing with correct dashboard url for ConsoleLink: %w", err)
	}

	err = deploy.DeployManifestsFromPath(cli, owner, PathConsoleLink, namespace, componentName, enabled)
	if err != nil {
		return fmt.Errorf("failed to set dashboard consolelink from %s: %w", PathConsoleLink, err)
	}

	return nil
}
