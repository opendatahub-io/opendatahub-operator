// Package dashboard provides utility functions to config Open Data Hub Dashboard: A web dashboard that displays
// installed Open Data Hub components with easy access to component UIs and documentation
// +groupName=datasciencecluster.opendatahub.io
package dashboard

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/go-logr/logr"
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

var (
	ComponentNameUpstream = "dashboard"
	PathUpstream          = deploy.DefaultManifestPath + "/" + ComponentNameUpstream + "/odh"

	ComponentNameDownstream = "rhods-dashboard"
	PathDownstream          = deploy.DefaultManifestPath + "/" + ComponentNameUpstream + "/rhoai"
	PathSelfDownstream      = PathDownstream + "/onprem"
	PathManagedDownstream   = PathDownstream + "/addon"
	OverridePath            = ""
)

// Verifies that Dashboard implements ComponentInterface.
var _ components.ComponentInterface = (*Dashboard)(nil)

// Dashboard struct holds the configuration for the Dashboard component.
// +kubebuilder:object:generate=true
type Dashboard struct {
	components.Component `json:""`
}

func (d *Dashboard) OverrideManifests(ctx context.Context, platform cluster.Platform) error {
	// If devflags are set, update default manifests path
	if len(d.DevFlags.Manifests) != 0 {
		manifestConfig := d.DevFlags.Manifests[0]
		if err := deploy.DownloadManifests(ctx, ComponentNameUpstream, manifestConfig); err != nil {
			return err
		}
		if manifestConfig.SourcePath != "" {
			OverridePath = filepath.Join(deploy.DefaultManifestPath, ComponentNameUpstream, manifestConfig.SourcePath)
		}
	}
	return nil
}

func (d *Dashboard) GetComponentName() string {
	return ComponentNameUpstream
}

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
		l = d.ConfigComponentLogger(logger, ComponentNameDownstream, dscispec)
	} else {
		l = d.ConfigComponentLogger(logger, ComponentNameUpstream, dscispec)
	}

	entryPath := map[cluster.Platform]string{
		cluster.SelfManagedRhods: PathDownstream + "/onprem",
		cluster.ManagedRhods:     PathDownstream + "/addon",
		cluster.OpenDataHub:      PathUpstream,
		cluster.Unknown:          PathUpstream,
	}[platform]

	enabled := d.GetManagementState() == operatorv1.Managed
	monitoringEnabled := dscispec.Monitoring.ManagementState == operatorv1.Managed
	imageParamMap := make(map[string]string)

	if enabled {
		// 1. cleanup OAuth client related secret and CR if dashboard is in 'installed false' status
		if err := d.cleanOauthClient(ctx, cli, dscispec, currentComponentExist, l); err != nil {
			return err
		}
		if d.DevFlags != nil && len(d.DevFlags.Manifests) != 0 {
			// Download manifests and update paths
			if err := d.OverrideManifests(ctx, platform); err != nil {
				return err
			}
			if OverridePath != "" {
				entryPath = OverridePath
			}
		} else { // Update image parameters if devFlags is not provided
			imageParamMap["odh-dashboard-image"] = "RELATED_IMAGE_ODH_DASHBOARD_IMAGE"
		}

		// 2. platform specific RBAC
		if platform == cluster.OpenDataHub || platform == "" {
			if err := cluster.UpdatePodSecurityRolebinding(ctx, cli, dscispec.ApplicationsNamespace, "odh-dashboard"); err != nil {
				return err
			}
		} else {
			if err := cluster.UpdatePodSecurityRolebinding(ctx, cli, dscispec.ApplicationsNamespace, "rhods-dashboard"); err != nil {
				return err
			}
		}

		// 3. Append or Update variable for component to consume
		extraParamsMap, err := updateKustomizeVariable(ctx, cli, platform, dscispec)
		if err != nil {
			return errors.New("failed to set variable for extraParamsMap")
		}

		// 4. update params.env regardless devFlags is provided of not
		if err := deploy.ApplyParams(entryPath, imageParamMap, extraParamsMap); err != nil {
			return fmt.Errorf("failed to update params.env  from %s : %w", entryPath, err)
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
		// Deploy RHOAI manifests
		if err := deploy.DeployManifestsFromPath(ctx, cli, owner, entryPath, dscispec.ApplicationsNamespace, ComponentNameDownstream, enabled); err != nil {
			return fmt.Errorf("failed to apply manifests from %s: %w", PathDownstream, err)
		}
		l.Info("apply manifests done")

		if enabled {
			if err := cluster.WaitForDeploymentAvailable(ctx, cli, ComponentNameDownstream, dscispec.ApplicationsNamespace, 20, 3); err != nil {
				return fmt.Errorf("deployment for %s is not ready to server: %w", ComponentNameDownstream, err)
			}
		}

		// CloudService Monitoring handling
		if platform == cluster.ManagedRhods {
			if err := d.UpdatePrometheusConfig(cli, l, enabled && monitoringEnabled, ComponentNameDownstream); err != nil {
				return err
			}
			if err := deploy.DeployManifestsFromPath(ctx, cli, owner,
				filepath.Join(deploy.DefaultManifestPath, "monitoring", "prometheus", "apps"),
				dscispec.Monitoring.Namespace,
				"prometheus", true); err != nil {
				return err
			}
			l.Info("updating SRE monitoring done")
		}
		return nil

	default:
		// Deploy ODH manifests
		if err := deploy.DeployManifestsFromPath(ctx, cli, owner, entryPath, dscispec.ApplicationsNamespace, ComponentNameUpstream, enabled); err != nil {
			return err
		}
		l.Info("apply manifests done")
		if enabled {
			if err := cluster.WaitForDeploymentAvailable(ctx, cli, ComponentNameUpstream, dscispec.ApplicationsNamespace, 20, 3); err != nil {
				return fmt.Errorf("deployment for %s is not ready to server: %w", ComponentNameUpstream, err)
			}
		}

		return nil
	}
}

func updateKustomizeVariable(ctx context.Context, cli client.Client, platform cluster.Platform, dscispec *dsciv1.DSCInitializationSpec) (map[string]string, error) {
	adminGroups := map[cluster.Platform]string{
		cluster.SelfManagedRhods: "rhods-admins",
		cluster.ManagedRhods:     "dedicated-admins",
		cluster.OpenDataHub:      "odh-admins",
		cluster.Unknown:          "odh-admins",
	}[platform]

	sectionTitle := map[cluster.Platform]string{
		cluster.SelfManagedRhods: "OpenShift Self Managed Services",
		cluster.ManagedRhods:     "OpenShift Managed Services",
		cluster.OpenDataHub:      "OpenShift Open Data Hub",
		cluster.Unknown:          "OpenShift Open Data Hub",
	}[platform]

	consoleLinkDomain, err := cluster.GetDomain(ctx, cli)
	if err != nil {
		return nil, fmt.Errorf("error getting console route URL %s : %w", consoleLinkDomain, err)
	}
	consoleURL := map[cluster.Platform]string{
		cluster.SelfManagedRhods: "https://rhods-dashboard-" + dscispec.ApplicationsNamespace + "." + consoleLinkDomain,
		cluster.ManagedRhods:     "https://rhods-dashboard-" + dscispec.ApplicationsNamespace + "." + consoleLinkDomain,
		cluster.OpenDataHub:      "https://odh-dashboard-" + dscispec.ApplicationsNamespace + "." + consoleLinkDomain,
		cluster.Unknown:          "https://odh-dashboard-" + dscispec.ApplicationsNamespace + "." + consoleLinkDomain,
	}[platform]

	return map[string]string{
		"admin_groups":  adminGroups,
		"dashboard-url": consoleURL,
		"section-title": sectionTitle,
	}, nil
}

func (d *Dashboard) cleanOauthClient(ctx context.Context, cli client.Client, dscispec *dsciv1.DSCInitializationSpec, currentComponentExist bool, l logr.Logger) error {
	// Remove previous oauth-client secrets
	// Check if component is going from state of `Not Installed --> Installed`
	// Assumption: Component is currently set to enabled
	name := "dashboard-oauth-client"
	if !currentComponentExist {
		l.Info("Cleanup any left secret")
		// Delete client secrets from previous installation
		oauthClientSecret := &corev1.Secret{}
		err := cli.Get(ctx, client.ObjectKey{
			Namespace: dscispec.ApplicationsNamespace,
			Name:      name,
		}, oauthClientSecret)
		if err != nil {
			if !k8serr.IsNotFound(err) {
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
