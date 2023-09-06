// Package codeflare provides utility functions to config CodeFlare as part of the stack which makes managing distributed compute infrastructure in the cloud easy and intuitive for Data Scientists
package codeflare

import (
	"fmt"

	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName              = "codeflare"
	CodeflarePath              = deploy.DefaultManifestPath + "/" + "codeflare-stack" + "/base"
	CodeflareOperator          = "codeflare-operator"
	RHCodeflareOperator        = "rhods-codeflare-operator"
	CodeflareOperatorNamespace = "openshift-operators"
)

var imageParamMap = map[string]string{}

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

func (d *CodeFlare) ReconcileComponent(owner metav1.Object, client client.Client, scheme *runtime.Scheme, managementState operatorv1.ManagementState, namespace string, manifestsUri string) error {
	enabled := managementState == operatorv1.Managed

	if enabled {
		// check if the CodeFlare operator is installed
		// codeflare operator not installed
		dependentOperator := CodeflareOperator
		dependentOperatorNamespace := CodeflareOperatorNamespace
		platform, err := deploy.GetPlatform(client)
		if err != nil {
			return err
		}
		// overwrite dependent operator if downstream not match upstream
		if platform == deploy.SelfManagedRhods || platform == deploy.ManagedRhods {
			dependentOperator = RHCodeflareOperator
		}
		found, err := deploy.SubscriptionExists(client, dependentOperatorNamespace, dependentOperator)
		if !found {
			if err != nil {
				return err
			} else {
				return fmt.Errorf("operator %s not found in namespace %s. Please install the operator before enabling %s component",
				dependentOperator, dependentOperatorNamespace, ComponentName)
			}
		}

		// Update image parameters only when we do not have customized manifests set
		if manifestsUri == "" {
			if err := deploy.ApplyImageParams(CodeflarePath, imageParamMap); err != nil {
				return err
			}
		}
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
