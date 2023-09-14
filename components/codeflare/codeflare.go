// Package codeflare provides utility functions to config CodeFlare as part of the stack which makes managing distributed compute infrastructure in the cloud easy and intuitive for Data Scientists
package codeflare

import (
	"fmt"
	operatorv1 "github.com/openshift/api/operator/v1"

	"context"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	imagev1 "github.com/openshift/api/image/v1"
	codeflarev1alpha1 "github.com/project-codeflare/codeflare-operator/api/codeflare/v1alpha1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName       = "codeflare"
	CodeflarePath       = deploy.DefaultManifestPath + "/" + "codeflare-stack" + "/base"
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

	// Special handling to delete MCAD InstaScale ImageStream resources
	if c.GetManagementState() == operatorv1.Removed {
		// Fetch the MCAD resource based on the request
		mcad := &codeflarev1alpha1.MCAD{}
		err := cli.Get(context.TODO(), client.ObjectKey{
			Name: "mcad",
		}, mcad)
		if err != nil {
			if apierrs.IsNotFound(err) {
				return fmt.Errorf("failed to get MCAD instance mcad: %v", err)
			}
		}
		err = cli.Delete(context.TODO(), mcad)

		// Fetch InstaScale based on the request
		instascale := &codeflarev1alpha1.InstaScale{}
		err = cli.Get(context.TODO(), client.ObjectKey{
			Name: "instascale",
		}, instascale)
		if err != nil {
			if apierrs.IsNotFound(err) {
				return fmt.Errorf("failed to get InstaScale instance instascale: %v", err)
			}
		}
		err = cli.Delete(context.TODO(), instascale)

		// Fetch Imagestream based on the request
		imagestream := &imagev1.ImageStream{}
		err = cli.Get(context.TODO(), client.ObjectKey{
			Name: "codeflare-notebook",
		}, imagestream)
		if err != nil {
			if apierrs.IsNotFound(err) {
				return fmt.Errorf("failed to get Imagestream instance codeflare-notebook: %v", err)
			}
		}
		err = cli.Delete(context.TODO(), imagestream)
	}

	// Deploy Codeflare
	err := deploy.DeployManifestsFromPath(owner, cli, c.GetComponentName(),
		CodeflarePath,
		dscispec.ApplicationsNamespace,
		cli.Scheme(), enabled)

	return err

}

func (c *CodeFlare) DeepCopyInto(target *CodeFlare) {
	*target = *c
	target.Component = c.Component
}
