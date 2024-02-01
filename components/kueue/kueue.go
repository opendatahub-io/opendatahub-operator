package kueue

import (
	"context"
	"fmt"
	"path/filepath"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/monitoring"
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

func (r *Kueue) OverrideManifests(_ string) error {
	// If devflags are set, update default manifests path
	if len(r.DevFlags.Manifests) != 0 {
		manifestConfig := r.DevFlags.Manifests[0]
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

func (r *Kueue) GetComponentName() string {
	return ComponentName
}

func (r *Kueue) ReconcileComponent(ctx context.Context, cli client.Client, resConf *rest.Config, owner metav1.Object, dscispec *dsciv1.DSCInitializationSpec, _ bool) error {
	var imageParamMap = map[string]string{
		"odh-kueue-controller-image": "RELATED_IMAGE_ODH_KUEUE_OPERATOR_IMAGE", // new kueue image
	}

	enabled := r.GetManagementState() == operatorv1.Managed
	monitoringEnabled := dscispec.Monitoring.ManagementState == operatorv1.Managed
	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}

	if enabled {
		if r.DevFlags != nil {
			// Download manifests and update paths
			if err = r.OverrideManifests(string(platform)); err != nil {
				return err
			}
		}
		if (dscispec.DevFlags == nil || dscispec.DevFlags.ManifestsUri == "") && (r.DevFlags == nil || len(r.DevFlags.Manifests) == 0) {
			if err := deploy.ApplyParams(Path, r.SetImageParamsMap(imageParamMap), true); err != nil {
				return err
			}
		}
	}
	// Deploy Kueue Operator
	if err := deploy.DeployManifestsFromPath(cli, owner, Path, dscispec.ApplicationsNamespace, ComponentName, enabled); err != nil {
		return err
	}
	// CloudService Monitoring handling
	if platform == deploy.ManagedRhods {
		if enabled {
			// first check if the service is up, so prometheus wont fire alerts when it is just startup
			if err := monitoring.WaitForDeploymentAvailable(ctx, resConf, ComponentName, dscispec.ApplicationsNamespace, 20, 2); err != nil {
				return fmt.Errorf("deployment for %s is not ready to server: %w", ComponentName, err)
			}
			fmt.Printf("deployment for %s is done, updating monitoring rules\n", ComponentName)
		}
		if err := r.UpdatePrometheusConfig(cli, enabled && monitoringEnabled, ComponentName); err != nil {
			return err
		}
		if err = deploy.DeployManifestsFromPath(cli, owner,
			filepath.Join(deploy.DefaultManifestPath, "monitoring", "prometheus", "apps"),
			dscispec.Monitoring.Namespace,
			"prometheus", true); err != nil {
			return err
		}
	}

	return nil
}
