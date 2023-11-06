// Package ray provides utility functions to config Ray as part of the stack
// which makes managing distributed compute infrastructure in the cloud easy and intuitive for Data Scientists
package ray

import (
	"path/filepath"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

var (
	ComponentName = "ray"
	RayPath       = deploy.DefaultManifestPath + "/ray/openshift"
)

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
		defaultKustomizePath := "operator/base"
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

// Verifies that Ray implements ComponentInterface.
var _ components.ComponentInterface = (*Ray)(nil)

func (r *Ray) ReconcileComponent(cli client.Client, owner metav1.Object, dscispec *dsci.DSCInitializationSpec) error {
	var imageParamMap = map[string]string{
		"odh-kuberay-operator-controller-image": "RELATED_IMAGE_ODH_KUBERAY_OPERATOR_CONTROLLER_IMAGE",
		"namespace":                             dscispec.ApplicationsNamespace,
	}

	enabled := r.GetManagementState() == operatorv1.Managed

	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}

	if enabled {
		// Download manifests and update paths
		if err = r.OverrideManifests(string(platform)); err != nil {
			return err
		}

		if dscispec.DevFlags.ManifestsUri == "" || len(r.DevFlags.Manifests) == 0 {
			if err := deploy.ApplyParams(RayPath, r.SetImageParamsMap(imageParamMap), true); err != nil {
				return err
			}
		}
	}
	// Deploy Ray Operator
	err = deploy.DeployManifestsFromPath(cli, owner, RayPath, dscispec.ApplicationsNamespace, r.GetComponentName(), enabled)
	return err
}

func (r *Ray) DeepCopyInto(target *Ray) {
	*target = *r
	target.Component = r.Component
}
