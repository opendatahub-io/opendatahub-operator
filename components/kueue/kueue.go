// +groupName=datasciencecluster.opendatahub.io
package kueue

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/go-logr/logr"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

var (
	ComponentName = "kueue"
	Path          = deploy.DefaultManifestPath + "/" + ComponentName + "/rhoai" // same path for both odh and rhoai
)

// Verifies that Kueue implements ComponentInterface.
var _ components.ComponentInterface = (*Kueue)(nil)

// Kueue struct holds the configuration for the Kueue component.
// +kubebuilder:object:generate=true
type Kueue struct {
	components.Component `json:""`
}

func (k *Kueue) OverrideManifests(_ string) error {
	// If devflags are set, update default manifests path
	if len(k.DevFlags.Manifests) != 0 {
		manifestConfig := k.DevFlags.Manifests[0]
		if err := deploy.DownloadManifests(ComponentName, manifestConfig); err != nil {
			return err
		}
		// If overlay is defined, update paths
		defaultKustomizePath := "openshift"
		if manifestConfig.SourcePath != "" {
			defaultKustomizePath = manifestConfig.SourcePath
		}
		Path = filepath.Join(deploy.DefaultManifestPath, ComponentName, defaultKustomizePath)
	}

	return nil
}

func (k *Kueue) GetComponentName() string {
	return ComponentName
}

func (k *Kueue) ReconcileComponent(ctx context.Context, cli client.Client, logger logr.Logger,
	owner metav1.Object, dscispec *dsciv1.DSCInitializationSpec, _ bool) error {
	l := k.ConfigComponentLogger(logger, ComponentName, dscispec)
	var imageParamMap = map[string]string{
		"odh-kueue-controller-image": "RELATED_IMAGE_ODH_KUEUE_CONTROLLER_IMAGE", // new kueue image
	}

	enabled := k.GetManagementState() == operatorv1.Managed
	monitoringEnabled := dscispec.Monitoring.ManagementState == operatorv1.Managed
	platform, err := cluster.GetPlatform(cli)
	if err != nil {
		return err
	}

	if enabled {
		if k.DevFlags != nil {
			// Download manifests and update paths
			if err = k.OverrideManifests(string(platform)); err != nil {
				return err
			}
		}
		if (dscispec.DevFlags == nil || dscispec.DevFlags.ManifestsUri == "") && (k.DevFlags == nil || len(k.DevFlags.Manifests) == 0) {
			if err := deploy.ApplyParams(Path, imageParamMap, true); err != nil {
				return fmt.Errorf("failed to update image from %s : %w", Path, err)
			}
		}
	}
	// Deploy Kueue Operator
	if err := deploy.DeployManifestsFromPath(cli, owner, Path, dscispec.ApplicationsNamespace, ComponentName, enabled); err != nil {
		return fmt.Errorf("failed to apply manifetss %s: %w", Path, err)
	}
	l.Info("apply manifests done")
	// CloudService Monitoring handling
	if platform == cluster.ManagedRhods {
		if enabled {
			// first check if the service is up, so prometheus won't fire alerts when it is just startup
			if err := cluster.WaitForDeploymentAvailable(ctx, cli, ComponentName, dscispec.ApplicationsNamespace, 20, 2); err != nil {
				return fmt.Errorf("deployment for %s is not ready to server: %w", ComponentName, err)
			}
			l.Info("deployment is done, updating monitoring rules")
		}
		if err := k.UpdatePrometheusConfig(cli, enabled && monitoringEnabled, ComponentName); err != nil {
			return err
		}
		if err = deploy.DeployManifestsFromPath(cli, owner,
			filepath.Join(deploy.DefaultManifestPath, "monitoring", "prometheus", "apps"),
			dscispec.Monitoring.Namespace,
			"prometheus", true); err != nil {
			return err
		}
		l.Info("updating SRE monitoring done")
	}

	return nil
}
