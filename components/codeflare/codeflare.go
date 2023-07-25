package codeflare

import (
	"fmt"
	"github.com/go-logr/logr"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName              = "codeflare"
	CodeflarePath              = deploy.DefaultManifestPath + "/" + "codeflare-stack" + "/base"
	CodeflareOperator          = "codeflare-operator"
	CodeflareOperatorNamespace = "openshift-operators"
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

func (d *CodeFlare) ReconcileComponent(
	owner metav1.Object,
	client client.Client,
	scheme *runtime.Scheme,
	enabled bool,
	namespace string,
	logger logr.Logger,
) error {

	if enabled {
		// check if the CodeFlare operator is installed
		// codeflare operator not installed
		found, err := deploy.SubscriptionExists(client, CodeflareOperatorNamespace, CodeflareOperator)
		if !found {
			if err != nil {
				return err
			} else {
				return fmt.Errorf("operator %s not found in namespace %s. Please install the operator before enabling %s component",
					CodeflareOperator, CodeflareOperatorNamespace, ComponentName)
			}
		}
	}

	// Update image parameters
	if err := deploy.ApplyImageParams(CodeflarePath, imageParamMap); err != nil {
		logger.Error(err, "Failed to replace image from params.env", "path", CodeflarePath)
		return err
	}

	// Deploy Codeflare
	err := deploy.DeployManifestsFromPath(owner, client, ComponentName,
		CodeflarePath,
		namespace,
		scheme, enabled, logger)
	if err != nil {
		logger.Error(err, "Failed to set Codeflare config", "path", CodeflarePath)
	}
	return err

}

func (in *CodeFlare) DeepCopyInto(out *CodeFlare) {
	*out = *in
	out.Component = in.Component
}
