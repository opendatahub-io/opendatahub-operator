package distributedworkloads

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName = "distributed-workloads"
	CodeflarePath = deploy.DefaultManifestPath + "/" + "codeflare-stack" + "/base"
	RayPath       = deploy.DefaultManifestPath + "/" + "ray/operator" + "/base"
)

var imageParamMap = map[string]string{}

type DistributedWorkloads struct {
	components.Component `json:""`
}

func (d *DistributedWorkloads) SetImageParamsMap(imageMap map[string]string) map[string]string {
	imageParamMap = imageMap
	return imageParamMap
}

func (d *DistributedWorkloads) GetComponentName() string {
	return ComponentName
}

// Verifies that Distributed Workloads implements ComponentInterface
var _ components.ComponentInterface = (*DistributedWorkloads)(nil)

func (d *DistributedWorkloads) ReconcileComponent(owner metav1.Object, client client.Client, scheme *runtime.Scheme, enabled bool, namespace string) error {

	// Update image parameters
	if err := deploy.ApplyImageParams(CodeflarePath, imageParamMap); err != nil {
		return err
	}

	// Deploy Codeflare
	err := deploy.DeployManifestsFromPath(owner, client, ComponentName,
		CodeflarePath,
		namespace,
		scheme, enabled)

	if err != nil {
		return err
	}

	// Update image parameters
	if err := deploy.ApplyImageParams(RayPath, imageParamMap); err != nil {
		return err
	}
	// Deploy Ray Operator
	err = deploy.DeployManifestsFromPath(owner, client, ComponentName,
		RayPath,
		namespace,
		scheme, enabled)
	return err

}

func (in *DistributedWorkloads) DeepCopyInto(out *DistributedWorkloads) {
	*out = *in
	out.Component = in.Component
}
