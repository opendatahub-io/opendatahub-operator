// Package ray provides utility functions to config Ray as part of the stack which makes managing distributed compute infrastructure in the cloud easy and intuitive for Data Scientists
package ray

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName = "ray"
	RayPath       = deploy.DefaultManifestPath + "/" + "ray/operator/base"
)

var imageParamMap = map[string]string{
	"odh-kuberay-operator-controller-image": "RELATED_IMAGE_ODH_KUBERAY_OPERATOR_CONTROLLER_IMAGE",
}

type Ray struct {
	components.Component `json:""`
}

func (r *Ray) SetImageParamsMap(imageMap map[string]string) map[string]string {
	imageParamMap = imageMap
	return imageParamMap
}

func (r *Ray) GetComponentName() string {
	return ComponentName
}

// Verifies that Ray implements ComponentInterface
var _ components.ComponentInterface = (*Ray)(nil)

func (r *Ray) ReconcileComponent(cli client.Client, owner metav1.Object, dsciInfo *components.DataScienceClusterConfig) error {
	enabled := r.GetManagementState() == operatorv1.Managed
	applicationsNamespace := dsciInfo.DSCISpec.ApplicationsNamespace
	notOverrideManifestsUri := dsciInfo.DSCISpec.DevFlags.ManifestsUri == ""

	if enabled {
		if notOverrideManifestsUri {
			if err := deploy.ApplyImageParams(RayPath, imageParamMap); err != nil {
				return err
			}
		}
	}
	// Deploy Ray Operator
	err := deploy.DeployManifestsFromPath(cli, owner, RayPath, applicationsNamespace, r.GetComponentName(), enabled)
	return err

}

func (r *Ray) DeepCopyInto(target *Ray) {
	*target = *r
	target.Component = r.Component
}
