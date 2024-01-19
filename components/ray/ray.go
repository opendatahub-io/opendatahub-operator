// Package ray provides utility functions to config Ray as part of the stack
// which makes managing distributed compute infrastructure in the cloud easy and intuitive for Data Scientists
package ray

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
	ComponentName = "ray"
	RayPath       = deploy.DefaultManifestPath + "/" + ComponentName + "/openshift"
)

// Verifies that Ray implements ComponentInterface.
var _ components.ComponentInterface = (*Ray)(nil)

// Ray struct holds the configuration for the Ray component.
// +kubebuilder:object:generate=true
type Ray struct {
	components.Component `json:""`
}

func (r *Ray) OverrideManifests(_ string) error {
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
		RayPath = filepath.Join(deploy.DefaultManifestPath, ComponentName, defaultKustomizePath)
	}
	return nil
}

func (r *Ray) GetComponentName() string {
	return ComponentName
}

func (r *Ray) ReconcileComponent(ctx context.Context, cli client.Client, resConf *rest.Config, owner metav1.Object, dscispec *dsciv1.DSCInitializationSpec, _ bool) error {
	var imageParamMap = map[string]string{
		"odh-kuberay-operator-controller-image": "RELATED_IMAGE_ODH_KUBERAY_OPERATOR_CONTROLLER_IMAGE",
		"namespace":                             dscispec.ApplicationsNamespace,
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
			if err := deploy.ApplyParams(RayPath, r.SetImageParamsMap(imageParamMap), true); err != nil {
				return err
			}
		}
	}
	// Deploy Ray Operator
	if err := deploy.DeployManifestsFromPath(cli, owner, RayPath, dscispec.ApplicationsNamespace, ComponentName, enabled); err != nil {
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
