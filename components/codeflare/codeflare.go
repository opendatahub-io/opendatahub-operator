package codeflare

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName = "codeflare"
	CodeflarePath = deploy.DefaultManifestPath + "/" + "codeflare-stack/base"
)

var imageParamMap = map[string]string{
	"odh-codeflare-operator-image": "RELATED_IMAGE_ODH_CODEFLARE_OPERATOR_IMAGE",
	"odh-mcad-controller-image":    "RELATED_IMAGE_ODH_MCAD_CONTROLLER_IMAGE",
}

type CodeFlare struct {
	components.Component `json:""`
}

func (d *CodeFlare) SetImageParamsMap(imageMap map[string]string) map[string]string {
	imageParamMap = imageMap
	return imageParamMap
}

func (d *CodeFlare) GetComponentName() string {
	return ComponentName
}

// Verifies that CodeFlare implements ComponentInterface
var _ components.ComponentInterface = (*CodeFlare)(nil)

func (d *CodeFlare) ReconcileComponent(owner metav1.Object, client client.Client, scheme *runtime.Scheme, enabled bool, namespace string) error {

	// Update image parameters
	if err := deploy.ApplyImageParams(CodeflarePath, imageParamMap); err != nil {
		return err
	}

	// Deploy Codeflare
	err := deploy.DeployManifestsFromPath(owner, client, ComponentName,
		CodeflarePath,
		namespace,
		scheme, enabled)

	return err

}

func (in *CodeFlare) DeepCopyInto(out *CodeFlare) {
	*out = *in
	out.Component = in.Component
}
