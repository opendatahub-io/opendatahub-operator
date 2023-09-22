// Package codeflare provides utility functions to config CodeFlare as part of the stack which makes managing distributed compute infrastructure in the cloud easy and intuitive for Data Scientists
package codeflare

import (
	"fmt"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName       = "codeflare"
	CodeflarePath       = deploy.DefaultManifestPath + "/codeflare/base"
	CodeflareOperator   = "codeflare-operator"
	RHCodeflareOperator = "rhods-codeflare-operator"
)

var imageParamMap = map[string]string{}

type CodeFlare struct {
	components.Component `json:""`
}

func (c *CodeFlare) SetImageParamsMap(imageMap map[string]string) map[string]string {
	imageParamMap = imageMap
	return imageParamMap
}

func (c *CodeFlare) GetComponentName() string {
	return ComponentName
}

// Verifies that CodeFlare implements ComponentInterface
var _ components.ComponentInterface = (*CodeFlare)(nil)

func (c *CodeFlare) ReconcileComponent(cli client.Client, owner metav1.Object, dscispec *dsci.DSCInitializationSpec) error {
	enabled := c.GetManagementState() == operatorv1.Managed

	if enabled {
		// check if the CodeFlare operator is installed
		// codeflare operator not installed
		dependentOperator := CodeflareOperator
		platform, err := deploy.GetPlatform(cli)
		if err != nil {
			return err
		}
		// overwrite dependent operator if downstream not match upstream
		if platform == deploy.SelfManagedRhods || platform == deploy.ManagedRhods {
			dependentOperator = RHCodeflareOperator
		}

		if found, err := deploy.OperatorExists(cli, dependentOperator); err != nil {
			return err
		} else if !found {
			return fmt.Errorf("operator %s not found. Please install the operator before enabling %s component",
				dependentOperator, ComponentName)
		}

		// Update image parameters only when we do not have customized manifests set
		if dscispec.DevFlags.ManifestsUri == "" {
			if err := deploy.ApplyImageParams(CodeflarePath, imageParamMap); err != nil {
				return err
			}
		}
	}

	// Deploy Codeflare
	err := deploy.DeployManifestsFromPath(cli, owner, CodeflarePath, dscispec.ApplicationsNamespace, c.GetComponentName(), enabled)

	return err

}

func (c *CodeFlare) DeepCopyInto(target *CodeFlare) {
	*target = *c
	target.Component = c.Component
}
