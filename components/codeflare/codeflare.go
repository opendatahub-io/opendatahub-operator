// Package codeflare provides utility functions to config CodeFlare as part of the stack which makes managing distributed compute infrastructure in the cloud easy and intuitive for Data Scientists
package codeflare

import (
	"fmt"

	"context"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	imagev1 "github.com/openshift/api/image/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	codeflarev1alpha1 "github.com/project-codeflare/codeflare-operator/api/codeflare/v1alpha1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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

func (d *CodeFlare) SetImageParamsMap(imageMap map[string]string) map[string]string {
	imageParamMap = imageMap
	return imageParamMap
}

func (d *CodeFlare) GetComponentName() string {
	return ComponentName
}

// Verifies that CodeFlare implements ComponentInterface
var _ components.ComponentInterface = (*CodeFlare)(nil)

func (c *CodeFlare) ReconcileComponent(owner metav1.Object, cli client.Client, scheme *runtime.Scheme, managementState operatorv1.ManagementState, dscispec *dsci.DSCInitializationSpec) error {
	enabled := managementState == operatorv1.Managed

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
		found, err := deploy.OperatorExists(cli, dependentOperator)

		if !found {
			if err != nil {
				return err
			} else {
				return fmt.Errorf("operator %s not found. Please install the operator before enabling %s component",
					dependentOperator, ComponentName)
			}
		}

		// Update image parameters only when we do not have customized manifests set
		if dscispec.DevFlags.ManifestsUri == "" {
			if err := deploy.ApplyImageParams(CodeflarePath, imageParamMap); err != nil {
				return err
			}
		}
	}

	// Special handling to delete MCAD InstaScale ImageStream resources
	if managementState == operatorv1.Removed {
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
	err := deploy.DeployManifestsFromPath(owner, cli, ComponentName,
		CodeflarePath,
		dscispec.ApplicationsNamespace,
		scheme, enabled)

	return err

}

func (in *CodeFlare) DeepCopyInto(out *CodeFlare) {
	*out = *in
	out.Component = in.Component
}
