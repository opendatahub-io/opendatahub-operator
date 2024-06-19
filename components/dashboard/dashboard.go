// Package dashboard provides utility functions to config Open Data Hub Dashboard: A web dashboard that displays
// installed Open Data Hub components with easy access to component UIs and documentation
// +groupName=datasciencecluster.opendatahub.io
package dashboard

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	v1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

var (
	ComponentName   = "dashboard"
	Path            = deploy.DefaultManifestPath + "/" + ComponentName + "/base"        // ODH
	PathISV         = deploy.DefaultManifestPath + "/" + ComponentName + "/apps"        // ODH APPS
	PathCRDs        = deploy.DefaultManifestPath + "/" + ComponentName + "/crd"         // ODH + RHOAI
	PathConsoleLink = deploy.DefaultManifestPath + "/" + ComponentName + "/consolelink" // ODH consolelink

	ComponentNameSupported   = "rhods-dashboard"
	PathSupported            = deploy.DefaultManifestPath + "/" + ComponentName + "/overlays/rhoai"              // RHOAI
	PathISVSM                = deploy.DefaultManifestPath + "/" + ComponentName + "/overlays/apps/apps-onprem"   // RHOAI APPS
	PathISVAddOn             = deploy.DefaultManifestPath + "/" + ComponentName + "/overlays/apps/apps-addon"    // RHOAI APPS
	PathConsoleLinkSupported = deploy.DefaultManifestPath + "/" + ComponentName + "/overlays/consolelink"        // RHOAI
	PathODHDashboardConfig   = deploy.DefaultManifestPath + "/" + ComponentName + "/overlays/odhdashboardconfig" // RHOAI odhdashboardconfig

	NameConsoleLink      = "console"
	NamespaceConsoleLink = "openshift-console"
)

// Verifies that Dashboard implements ComponentInterface.
var _ components.ComponentInterface = (*Dashboard)(nil)

// Dashboard struct holds the configuration for the Dashboard component.
// +kubebuilder:object:generate=true
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
		if platform == string(cluster.ManagedRhods) || platform == string(cluster.SelfManagedRhods) {
			defaultKustomizePath := "overlays/rhoai"
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

//nolint:gocyclo
func (d *Dashboard) ReconcileComponent(ctx context.Context,
	cli client.Client,
	logger logr.Logger,
	owner metav1.Object,
	dscispec *dsciv1.DSCInitializationSpec,
	platform cluster.Platform,
	currentComponentExist bool,
) error {
	var l logr.Logger

	if platform == cluster.SelfManagedRhods || platform == cluster.ManagedRhods {
		l = d.ConfigComponentLogger(logger, ComponentNameSupported, dscispec)
	} else {
		l = d.ConfigComponentLogger(logger, ComponentName, dscispec)
	}

	var imageParamMap = map[string]string{
		"odh-dashboard-image": "RELATED_IMAGE_ODH_DASHBOARD_IMAGE",
	}
	enabled := d.GetManagementState() == operatorv1.Managed
	monitoringEnabled := dscispec.Monitoring.ManagementState == operatorv1.Managed

	// Update Default rolebinding
	if enabled {
		// Update Default rolebinding
		// cleanup OAuth client related secret and CR if dashboard is in 'installed false' status
		if err := d.cleanOauthClient(ctx, cli, dscispec, currentComponentExist, l); err != nil {
			return err
		}
		if d.DevFlags != nil {
			// Download manifests and update paths
			if err := d.OverrideManifests(string(platform)); err != nil {
				return err
			}
		}
		// 1. Deploy CRDs
		if err := d.deployCRDsForPlatform(cli, owner, dscispec.ApplicationsNamespace, platform); err != nil {
			return fmt.Errorf("failed to deploy Dashboard CRD: %w", err)
		}

		// 2. platform specific RBAC
		if platform == cluster.OpenDataHub || platform == "" {
			err := cluster.UpdatePodSecurityRolebinding(ctx, cli, dscispec.ApplicationsNamespace, "odh-dashboard")
			if err != nil {
				return err
			}
		}

		if platform == cluster.SelfManagedRhods || platform == cluster.ManagedRhods {
			err := cluster.UpdatePodSecurityRolebinding(ctx, cli, dscispec.ApplicationsNamespace, "rhods-dashboard")
			if err != nil {
				return err
			}
		}

		// 3. Update image parameters
		if (dscispec.DevFlags == nil || dscispec.DevFlags.ManifestsUri == "") && (d.DevFlags == nil || len(d.DevFlags.Manifests) == 0) {
			if err := deploy.ApplyParams(PathSupported, imageParamMap, false); err != nil {
				return fmt.Errorf("failed to update image from %s : %w", PathSupported, err)
			}
		}
	}

	// common: Deploy odh-dashboard manifests
	// TODO: check if we can have the same component name odh-dashboard for both, or still keep rhods-dashboard for RHOAI
	switch platform {
	case cluster.SelfManagedRhods, cluster.ManagedRhods:
		// anaconda
		if err := cluster.CreateSecret(ctx, cli, "anaconda-ce-access", dscispec.ApplicationsNamespace); err != nil {
			return fmt.Errorf("failed to create access-secret for anaconda: %w", err)
		}
		// overlay which including ../../base + anaconda-ce-validator
		if err := deploy.DeployManifestsFromPath(cli, owner, PathSupported, dscispec.ApplicationsNamespace, ComponentNameSupported, enabled); err != nil {
			return fmt.Errorf("failed to apply manifests from %s: %w", PathSupported, err)
		}

		// Apply RHOAI specific configs, e.g anaconda screct and cronjob and ISV
		if err := d.applyRHOAISpecificConfigs(cli, owner, dscispec.ApplicationsNamespace, platform); err != nil {
			return err
		}
		// consolelink
		if err := d.deployConsoleLink(ctx, cli, owner, platform, dscispec.ApplicationsNamespace, ComponentNameSupported); err != nil {
			return err
		}
		l.Info("apply manifests done")

		// CloudService Monitoring handling
		if platform == cluster.ManagedRhods {
			if enabled {
				// first check if the service is up, so prometheus won't fire alerts when it is just startup
				if err := cluster.WaitForDeploymentAvailable(ctx, cli, ComponentNameSupported, dscispec.ApplicationsNamespace, 20, 3); err != nil {
					return fmt.Errorf("deployment for %s is not ready to server: %w", ComponentName, err)
				}
				l.Info("deployment is done, updating monitoring rules")
			}

			if err := d.UpdatePrometheusConfig(cli, enabled && monitoringEnabled, ComponentNameSupported); err != nil {
				return err
			}
			if err := deploy.DeployManifestsFromPath(cli, owner,
				filepath.Join(deploy.DefaultManifestPath, "monitoring", "prometheus", "apps"),
				dscispec.Monitoring.Namespace,
				"prometheus", true); err != nil {
				return err
			}
			l.Info("updating SRE monitoring done")
		}
		return nil
	default:
		// base
		if err := deploy.DeployManifestsFromPath(cli, owner, Path, dscispec.ApplicationsNamespace, ComponentName, enabled); err != nil {
			return err
		}
		// ISV
		if err := deploy.DeployManifestsFromPath(cli, owner, PathISV, dscispec.ApplicationsNamespace, ComponentName, enabled); err != nil {
			return err
		}
		// consolelink
		if err := d.deployConsoleLink(ctx, cli, owner, platform, dscispec.ApplicationsNamespace, ComponentName); err != nil {
			return err
		}
		l.Info("apply manifests done")
		return nil
	}
}

func (d *Dashboard) deployCRDsForPlatform(cli client.Client, owner metav1.Object, namespace string, platform cluster.Platform) error {
	componentName := ComponentName
	if platform == cluster.SelfManagedRhods || platform == cluster.ManagedRhods {
		componentName = ComponentNameSupported
	}
	// we only deploy CRD, we do not remove CRD
	return deploy.DeployManifestsFromPath(cli, owner, PathCRDs, namespace, componentName, true)
}

func (d *Dashboard) applyRHOAISpecificConfigs(cli client.Client, owner metav1.Object, namespace string, platform cluster.Platform) error {
	enabled := d.ManagementState == operatorv1.Managed

	// set proper group name
	dashboardConfig := filepath.Join(PathODHDashboardConfig, "odhdashboardconfig.yaml")
	adminGroups := map[cluster.Platform]string{
		cluster.SelfManagedRhods: "rhods-admins",
		cluster.ManagedRhods:     "dedicated-admins",
	}[platform]

	if err := common.ReplaceStringsInFile(dashboardConfig, map[string]string{"<admin_groups>": adminGroups}); err != nil {
		return err
	}
	if err := deploy.DeployManifestsFromPath(cli, owner, PathODHDashboardConfig, namespace, ComponentNameSupported, enabled); err != nil {
		return fmt.Errorf("failed to create OdhDashboardConfig from %s: %w", PathODHDashboardConfig, err)
	}
	// ISV
	path := PathISVSM
	if platform == cluster.ManagedRhods {
		path = PathISVAddOn
	}
	if err := deploy.DeployManifestsFromPath(cli, owner, path, namespace, ComponentNameSupported, enabled); err != nil {
		return fmt.Errorf("failed to set dashboard ISV from %s : %w", Path, err)
	}
	return nil
}

func (d *Dashboard) deployConsoleLink(ctx context.Context, cli client.Client, owner metav1.Object, platform cluster.Platform, namespace, componentName string) error {
	var manifestsPath, sectionTitle, routeName string
	switch platform {
	case cluster.SelfManagedRhods:
		sectionTitle = "OpenShift Self Managed Services"
		manifestsPath = PathConsoleLinkSupported
		routeName = componentName
	case cluster.ManagedRhods:
		sectionTitle = "OpenShift Managed Services"
		manifestsPath = PathConsoleLinkSupported
		routeName = componentName
	default:
		sectionTitle = "OpenShift Open Data Hub"
		manifestsPath = PathConsoleLink
		routeName = "odh-dashboard"
	}

	pathConsoleLink := filepath.Join(manifestsPath, "consolelink.yaml")

	consoleRoute := &routev1.Route{}
	if err := cli.Get(ctx, client.ObjectKey{Name: NameConsoleLink, Namespace: NamespaceConsoleLink}, consoleRoute); err != nil {
		return fmt.Errorf("error getting console route URL %s : %w", NameConsoleLink, err)
	}

	domainIndex := strings.Index(consoleRoute.Spec.Host, ".")
	consoleLinkDomain := consoleRoute.Spec.Host[domainIndex+1:]
	if err := common.ReplaceStringsInFile(pathConsoleLink, map[string]string{
		"<dashboard-url>": "https://" + routeName + "-" + namespace + "." + consoleLinkDomain,
		"<section-title>": sectionTitle,
	}); err != nil {
		return fmt.Errorf("error replacing with correct dashboard URL for consolelink : %w", err)
	}

	enabled := d.ManagementState == operatorv1.Managed
	if err := deploy.DeployManifestsFromPath(cli, owner, manifestsPath, namespace, componentName, enabled); err != nil {
		return fmt.Errorf("failed to set dashboard consolelink from %s: %w", manifestsPath, err)
	}

	return nil
}

func (d *Dashboard) cleanOauthClient(ctx context.Context, cli client.Client, dscispec *dsciv1.DSCInitializationSpec, currentComponentExist bool, l logr.Logger) error {
	// Remove previous oauth-client secrets
	// Check if component is going from state of `Not Installed --> Installed`
	// Assumption: Component is currently set to enabled
	name := "dashboard-oauth-client"
	if !currentComponentExist {
		fmt.Println("Cleanup any left secret")
		// Delete client secrets from previous installation
		oauthClientSecret := &v1.Secret{}
		err := cli.Get(ctx, client.ObjectKey{
			Namespace: dscispec.ApplicationsNamespace,
			Name:      name,
		}, oauthClientSecret)
		if err != nil {
			if !apierrs.IsNotFound(err) {
				return fmt.Errorf("error getting secret %s: %w", name, err)
			}
		} else {
			if err := cli.Delete(ctx, oauthClientSecret); err != nil {
				return fmt.Errorf("error deleting secret %s: %w", name, err)
			}
			l.Info("successfully deleted secret", "secret", name)
		}
	}
	return nil
}
