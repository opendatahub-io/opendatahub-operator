// Package workbenches provides utility functions to config Workbenches to secure Jupyter Notebook in Kubernetes environments with support for OAuth
package workbenches

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName = "workbenches"
	// manifests for nbc in ODH and downstream + downstream use it for imageparams
	notebookControllerPath = deploy.DefaultManifestPath + "/odh-notebook-controller/odh-notebook-controller/base"
	// manifests for ODH nbc
	kfnotebookControllerPath = deploy.DefaultManifestPath + "/odh-notebook-controller/kf-notebook-controller/overlays/openshift"
	// ODH image
	notebookImagesPath = deploy.DefaultManifestPath + "/notebook/overlays/additional"
	// downstream image
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

func (w *Workbenches) ReconcileComponent(cli client.Client, owner metav1.Object, dsciInfo *components.DataScienceClusterConfig) error {
	enabled := w.GetManagementState() == operatorv1.Managed
	platform := dsciInfo.Platform
	applicationsNamespace := dsciInfo.DSCISpec.ApplicationsNamespace
	notOverrideManifestsUri := dsciInfo.DSCISpec.DevFlags.ManifestsUri == ""

	// Set default notebooks namespace
	// Create rhods-notebooks namespace in managed platforms
	if enabled {
		if platform == deploy.SelfManagedRhods || platform == deploy.ManagedRhods {
			err := common.CreateNamespace(cli, "rhods-notebooks")
			if err != nil {
				// no need to log error as it was already logged in createOdhNamespace
				return err
			}
		}
		// Update Default rolebinding
		err := common.UpdatePodSecurityRolebinding(cli, []string{"notebook-controller-service-account"}, applicationsNamespace)
		if err != nil {
			return err
		}
		// Update image parameters for notebook controller
		if notOverrideManifestsUri {
			if err := deploy.ApplyImageParams(notebookControllerPath, imageParamMap); err != nil {
				return err
			}
		}
	}
	err := deploy.DeployManifestsFromPath(cli, owner, notebookControllerPath, applicationsNamespace, ComponentName, enabled)
	if err != nil {
		return err
	}

	// Update image parameters for nbc in downstream
	if enabled {
		if notOverrideManifestsUri {
			if platform == deploy.ManagedRhods || platform == deploy.SelfManagedRhods {
				if err := deploy.ApplyImageParams(notebookControllerPath, imageParamMap); err != nil {
					return err
				}
			}
		}
	}

	if platform == deploy.OpenDataHub || platform == "" {
		// only for ODH after transit to kubeflow repo
		err = deploy.DeployManifestsFromPath(cli, owner,
			kfnotebookControllerPath,
			applicationsNamespace,
			ComponentName, enabled)
		if err != nil {
			return err
		}

		err = deploy.DeployManifestsFromPath(cli, owner,
			notebookImagesPath,
			applicationsNamespace,
			ComponentName,
			enabled)
		return err
	} else {
		err = deploy.DeployManifestsFromPath(cli, owner,
			notebookImagesPathSupported,
			applicationsNamespace,
			ComponentName,
			enabled)
		return err
	}

}

func (w *Workbenches) DeepCopyInto(target *Workbenches) {
	*target = *w
	target.Component = w.Component
}
