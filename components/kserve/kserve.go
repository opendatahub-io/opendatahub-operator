// Package kserve provides utility functions to config Kserve as the Controller for serving ML models on arbitrary frameworks
package kserve

import (
	"fmt"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

const (
	ComponentName          = "kserve"
	Path                   = deploy.DefaultManifestPath + "/" + ComponentName + "/base"
	DependentComponentName = "odh-model-controller"
	DependentPath          = deploy.DefaultManifestPath + "/" + DependentComponentName + "/base"
	ServiceMeshOperator    = "servicemeshoperator"
	ServerlessOperator     = "serverless-operator"
)

// Kserve to use
var imageParamMap = map[string]string{}

// odh-model-controller to use
var dependentImageParamMap = map[string]string{
	"odh-model-controller": "RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE",
}

type Kserve struct {
	components.Component `json:""`
}

func (k *Kserve) SetImageParamsMap(imageMap map[string]string) map[string]string {
	imageParamMap = imageMap
	return imageParamMap
}

func (k *Kserve) GetComponentName() string {
	return ComponentName
}

// Verifies that Kserve implements ComponentInterface
var _ components.ComponentInterface = (*Kserve)(nil)

func (k *Kserve) ReconcileComponent(cli client.Client, owner metav1.Object, dscispec *dsci.DSCInitializationSpec) error {
	enabled := k.GetManagementState() == operatorv1.Managed

	if enabled {
		// check on dependent operators
		if found, err := deploy.OperatorExists(cli, ServiceMeshOperator); err != nil {
			return err
		} else if !found {
			return fmt.Errorf("operator %s not found. Please install the operator before enabling %s component",
				ServiceMeshOperator, ComponentName)
		}

		// check on dependent operators might be in multiple namespaces
		if found, err := deploy.OperatorExists(cli, ServerlessOperator); err != nil {
			return err
		} else if !found {
			return fmt.Errorf("operator %s not found. Please install the operator before enabling %s component",
				ServerlessOperator, ComponentName)
		}

		// Update image parameters only when we do not have customized manifests set
		if dscispec.DevFlags.ManifestsUri == "" {
			if err := deploy.ApplyImageParams(Path, imageParamMap); err != nil {
				return err
			}
		}
	}

	if err := deploy.DeployManifestsFromPath(owner, cli, ComponentName,
		Path,
		dscispec.ApplicationsNamespace,
		cli.Scheme(), enabled); err != nil {
		return err
	}

	// For odh-model-controller
	if enabled {
		err := common.UpdatePodSecurityRolebinding(cli, []string{"odh-model-controller"}, dscispec.ApplicationsNamespace)
		if err != nil {
			return err
		}
		// Update image parameters for keserve
		if dscispec.DevFlags.ManifestsUri == "" {
			if err := deploy.ApplyImageParams(Path, dependentImageParamMap); err != nil {
				return err
			}
		}
	}
	if err := deploy.DeployManifestsFromPath(owner, cli, k.GetComponentName(),
		DependentPath,
		dscispec.ApplicationsNamespace,
		cli.Scheme(), enabled); err != nil {
		return err
	}

	return nil
}

func (in *Kserve) DeepCopyInto(out *Kserve) {
	*out = *in
	out.Component = in.Component
}
