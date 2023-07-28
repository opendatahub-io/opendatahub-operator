package ray

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName = "ray"
	RayPath       = deploy.DefaultManifestPath + "/" + "ray/operator" + "/base"
)

var imageParamMap = map[string]string{}

type Ray struct {
	components.Component `json:""`
}

func (d *Ray) SetImageParamsMap(imageMap map[string]string) map[string]string {
	imageParamMap = imageMap
	return imageParamMap
}

func (d *Ray) GetComponentName() string {
	return ComponentName
}

// Verifies that Ray implements ComponentInterface
var _ components.ComponentInterface = (*Ray)(nil)

func (d *Ray) ReconcileComponent(owner metav1.Object, client client.Client, scheme *runtime.Scheme, enabled bool, namespace string) error {

	// Update image parameters
	if err := deploy.ApplyImageParams(RayPath, imageParamMap); err != nil {
		return err
	}
	// Deploy Ray Operator
	err := deploy.DeployManifestsFromPath(owner, client, ComponentName,
		RayPath,
		namespace,
		scheme, enabled)
	return err

}

func (in *Ray) DeepCopyInto(out *Ray) {
	*out = *in
	out.Component = in.Component
}
