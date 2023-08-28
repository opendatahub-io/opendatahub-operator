package kserve

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	operatorv1 "github.com/openshift/api/operator/v1"
)

const (
	ComponentName          = "kserve"
	Path                   = deploy.DefaultManifestPath + "/" + ComponentName + "/base"
	DependentComponentName = "odh-model-controller"
	DependentPath          = deploy.DefaultManifestPath + "/" + DependentComponentName + "/base"
)

var imageParamMap = map[string]string{
	"kserve-router":              "RELATED_IMAGE_ODH_KSERVE_ROUTE_IMAGE",
	"kserve-agent":               "RELATED_IMAGE_ODH_KSERVE_AGENT_IMAGE",
	"kserve-controller":          "RELATED_IMAGE_ODH_KSERVE_CONTROLLER_IMAGE",
	"kserve-storage-initializer": "RELATED_IMAGE_ODH_KSERVE_STORAGE_INITIALIZER_IMAGE",
}

var dependentImageParamMap = map[string]string{
	"odh-model-controller": "RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE",
}

type Kserve struct {
	components.Component `json:""`
}

func (d *Kserve) SetImageParamsMap(imageMap map[string]string) map[string]string {
	imageParamMap = imageMap
	return imageParamMap
}

func (d *Kserve) GetComponentName() string {
	return ComponentName
}

// Verifies that Kserve implements ComponentInterface
var _ components.ComponentInterface = (*Kserve)(nil)

func (d *Kserve) ReconcileComponent(owner metav1.Object, cli client.Client, scheme *runtime.Scheme, managementState operatorv1.ManagementState, namespace string) error {
	enabled := managementState == operatorv1.Managed

	// Update image parameters
	if err := deploy.ApplyImageParams(Path, imageParamMap); err != nil {
		return err
	}

	if err := deploy.DeployManifestsFromPath(owner, cli, ComponentName,
		Path,
		namespace,
		scheme, enabled); err != nil {
		return err
	}

	if enabled {
		err := common.UpdatePodSecurityRolebinding(cli, []string{"odh-model-controller"}, namespace)
		if err != nil {
			return err
		}
	}

	// Update image parameters
	if err := deploy.ApplyImageParams(Path, dependentImageParamMap); err != nil {
		return err
	}

	if err := deploy.DeployManifestsFromPath(owner, cli, ComponentName,
		DependentPath,
		namespace,
		scheme, enabled); err != nil {
		return err
	}
	return nil

}

func (in *Kserve) DeepCopyInto(out *Kserve) {
	*out = *in
	out.Component = in.Component
}
