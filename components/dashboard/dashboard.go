package dashboard

import (
	"fmt"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName = "odh-dashboard"
	Path          = deploy.DefaultManifestPath + "/" + ComponentName + "/base"
	PathISVSM     = deploy.DefaultManifestPath + "/" + ComponentName + "/overlays/apps-onpre"
	PathISVAddOn  = deploy.DefaultManifestPath + "/" + ComponentName + "/overlays/apps-addon"
	PathOVMS      = deploy.DefaultManifestPath + "/" + ComponentName + "/modelserving"
)

var imageParamMap = map[string]string{
	"odh-dashboard-image": "RELATED_IMAGE_ODH_DASHBOARD_IMAGE",
}

type Dashboard struct {
	components.Component `json:""`
}

func (d *Dashboard) SetImageParamsMap(imageMap map[string]string) map[string]string {
	imageParamMap = imageMap
	return imageParamMap
}

func (d *Dashboard) GetComponentName() string {
	return ComponentName
}

// Verifies that Dashboard implements ComponentInterface
var _ components.ComponentInterface = (*Dashboard)(nil)

func (d *Dashboard) ReconcileComponent(owner metav1.Object, cli client.Client, scheme *runtime.Scheme, enabled bool, namespace string) error {

	// TODO: Add any additional tasks if required when reconciling component
	// Update Default rolebinding
	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return fmt.Errorf(err.Error())
	}
	if platform == deploy.OpenDataHub {
		err := common.UpdatePodSecurityRolebinding(cli, []string{"odh-dashboard"}, namespace)
		if err != nil {
			return fmt.Errorf(err.Error())
		}
	} else {
		err := common.UpdatePodSecurityRolebinding(cli, []string{"rhods-dashboard"}, namespace)
		if err != nil {
			return fmt.Errorf(err.Error())
		}
	}

	// Update image parameters
	if err := deploy.ApplyImageParams(Path, imageParamMap); err != nil {
		return err
	}

	err = deploy.DeployManifestsFromPath(owner, cli, ComponentName,
		Path,
		namespace,
		scheme, enabled)
	if err != nil {
		return err
	}

	// OVMS
	if platform, _ := deploy.GetPlatform(cli); platform != deploy.OpenDataHub {
		err = deploy.DeployManifestsFromPath(owner, cli, ComponentName,
			PathOVMS,
			namespace,
			scheme, enabled)
		if err != nil {
			return fmt.Errorf("failed to set dashboard OVMS from %s: %v", PathOVMS, err)
		}
	}

	// ISV handling
	switch platform {
	case deploy.SelfManagedRhods:
		err = deploy.DeployManifestsFromPath(owner, cli, ComponentName,
			PathISVSM,
			namespace,
			scheme, enabled)
		if err != nil {
			return fmt.Errorf("failed to set dashboard ISV from %s: %v", PathISVSM, err)
		}
		return err
	case deploy.ManagedRhods:
		err = deploy.DeployManifestsFromPath(owner, cli, ComponentName,
			PathISVAddOn,
			namespace,
			scheme, enabled)
		if err != nil {
			return fmt.Errorf("failed to set dashboard ISV from %s: %v", PathISVAddOn, err)
		}
		return err
	default:
		return nil
	}

}

func (in *Dashboard) DeepCopyInto(out *Dashboard) {
	*out = *in
	out.Component = in.Component
}
