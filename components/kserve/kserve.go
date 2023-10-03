// Package kserve provides utility functions to config Kserve as the Controller for serving ML models on arbitrary frameworks
package kserve

import (
	"fmt"

	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"path/filepath"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

var (
	ComponentName          = "kserve"
	Path                   = deploy.DefaultManifestPath + "/" + ComponentName + "/base"
	DependentComponentName = "odh-model-controller"
	DependentPath          = deploy.DefaultManifestPath + "/" + DependentComponentName + "/base"
	ServiceMeshOperator    = "servicemeshoperator"
	ServerlessOperator     = "serverless-operator"
)

// Kserve to use.
var imageParamMap = map[string]string{}

// odh-model-controller to use.
var dependentImageParamMap = map[string]string{
	"odh-model-controller": "RELATED_IMAGE_ODH_MODEL_CONTROLLER_IMAGE",
}

type Kserve struct {
	components.Component `json:""`
}

func (k *Kserve) OverrideManifests(_ string) error {
	// Download manifests if defined by devflags
	if len(k.DevFlags.Manifests) != 0 {
		// Go through each manifests and set the overlays if defined
		for _, subcomponent := range k.DevFlags.Manifests {
			if strings.Contains(subcomponent.URI, DependentComponentName) {
				// Download subcomponent
				if err := deploy.DownloadManifests(DependentComponentName, subcomponent); err != nil {
					return err
				}
				// If overlay is defined, update paths
				defaultKustomizePath := "base"
				if subcomponent.SourcePath != "" {
					defaultKustomizePath = subcomponent.SourcePath
				}
				DependentPath = filepath.Join(deploy.DefaultManifestPath, DependentComponentName, defaultKustomizePath)
			}

			if strings.Contains(subcomponent.URI, ComponentName) {
				// Download subcomponent
				if err := deploy.DownloadManifests(ComponentName, subcomponent); err != nil {
					return err
				}
				// If overlay is defined, update paths
				defaultKustomizePath := "base"
				if subcomponent.SourcePath != "" {
					defaultKustomizePath = subcomponent.SourcePath
				}
				Path = filepath.Join(deploy.DefaultManifestPath, ComponentName, defaultKustomizePath)
			}
		}
	}
	return nil
}

func (k *Kserve) GetComponentDevFlags() components.DevFlags {
	return k.DevFlags
}

func (k *Kserve) SetImageParamsMap(imageMap map[string]string) map[string]string {
	imageParamMap = imageMap
	return imageParamMap
}

func (k *Kserve) GetComponentName() string {
	return ComponentName
}

// Verifies that Kserve implements ComponentInterface.
var _ components.ComponentInterface = (*Kserve)(nil)

func (k *Kserve) ReconcileComponent(cli client.Client, owner metav1.Object, dscispec *dsci.DSCInitializationSpec) error {
	enabled := k.GetManagementState() == operatorv1.Managed
	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}

	if enabled {
		// Download manifests and update paths
		if err = k.OverrideManifests(string(platform)); err != nil {
			return err
		}

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

	if err := deploy.DeployManifestsFromPath(cli, owner, Path, dscispec.ApplicationsNamespace, ComponentName, enabled); err != nil {
		return err
	}

	// For odh-model-controller
	if enabled {
		err := common.UpdatePodSecurityRolebinding(cli, []string{"odh-model-controller"}, dscispec.ApplicationsNamespace)
		if err != nil {
			return err
		}
		// Update image parameters for odh-maodel-controller
		if dscispec.DevFlags.ManifestsUri == "" {
			if err := deploy.ApplyImageParams(DependentPath, dependentImageParamMap); err != nil {
				return err
			}
		}
	}
	if err := deploy.DeployManifestsFromPath(cli, owner, DependentPath, dscispec.ApplicationsNamespace, k.GetComponentName(), enabled); err != nil {
		return err
	}

	return nil
}

func (k *Kserve) DeepCopyInto(target *Kserve) {
	*target = *k
	target.Component = k.Component
}
