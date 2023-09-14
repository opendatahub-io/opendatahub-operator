// Package workbenches provides utility functions to config Workbenches to secure Jupyter Notebook in Kubernetes environments with support for OAuth
package workbenches

import (
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName               = "workbenches"
	notebookControllerPath      = deploy.DefaultManifestPath + "/odh-notebook-controller/base"
	notebookImagesPath          = deploy.DefaultManifestPath + "/notebook-images/overlays/additional"
	notebookImagesPathSupported = deploy.DefaultManifestPath + "/jupyterhub/notebook-images/overlays/additional"
)

var imageParamMap = map[string]string{
	"odh-notebook-controller-image":    "RELATED_IMAGE_ODH_NOTEBOOK_CONTROLLER_IMAGE",
	"odh-kf-notebook-controller-image": "RELATED_IMAGE_ODH_KF_NOTEBOOK_CONTROLLER_IMAGE",
}

type Workbenches struct {
	components.Component `json:""`
}

func (w *Workbenches) GetComponentName() string {
	return ComponentName
}

func (w *Workbenches) SetImageParamsMap(imageMap map[string]string) map[string]string {
	imageParamMap = imageMap
	return imageParamMap
}

// Verifies that Dashboard implements ComponentInterface
var _ components.ComponentInterface = (*Workbenches)(nil)

func (w *Workbenches) ReconcileComponent(cli client.Client, owner metav1.Object, dscispec *dsci.DSCInitializationSpec) error {
	// Set default notebooks namespace
	// Create rhods-notebooks namespace in managed platforms
	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}

	enabled := w.GetManagementState() == operatorv1.Managed

	if enabled {
		if platform == deploy.SelfManagedRhods || platform == deploy.ManagedRhods {
			err := common.CreateNamespace(cli, "rhods-notebooks")
			if err != nil {
				// no need to log error as it was already logged in createOdhNamespace
				return err
			}
		}
		// Update Default rolebinding
		err = common.UpdatePodSecurityRolebinding(cli, []string{"notebook-controller-service-account"}, dscispec.ApplicationsNamespace)
		if err != nil {
			return err
		}
		// Update image parameters for notebook controller
		if dscispec.DevFlags.ManifestsUri == "" {
			if err := deploy.ApplyImageParams(notebookControllerPath, imageParamMap); err != nil {
				return err
			}
		}
	}

	err = deploy.DeployManifestsFromPath(owner, cli, ComponentName,
		notebookControllerPath,
		dscispec.ApplicationsNamespace,
		cli.Scheme(), enabled)
	if err != nil {
		return err
	}

	// Update image parameters for notebook image
	if enabled {
		if dscispec.DevFlags.ManifestsUri == "" {
			if platform == deploy.OpenDataHub || platform == "" {
				if err := deploy.ApplyImageParams(notebookImagesPath, imageParamMap); err != nil {
					return err
				}
			} else {
				if err := deploy.ApplyImageParams(notebookImagesPathSupported, imageParamMap); err != nil {
					return err
				}
			}
		}
	}

	if platform == deploy.OpenDataHub || platform == "" {
		err = deploy.DeployManifestsFromPath(owner, cli, ComponentName,
			notebookImagesPath,
			dscispec.ApplicationsNamespace,
			cli.Scheme(), enabled)
		return err
	} else {
		err = deploy.DeployManifestsFromPath(owner, cli, ComponentName,
			notebookImagesPathSupported,
			dscispec.ApplicationsNamespace,
			cli.Scheme(), enabled)
		return err
	}

}

func (w *Workbenches) DeepCopyInto(target *Workbenches) {
	*target = *w
	target.Component = w.Component
}
