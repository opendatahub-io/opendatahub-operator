package workbenches

import (
	"github.com/go-logr/logr"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName          = "workbenches"
	notebookControllerPath = deploy.DefaultManifestPath + "/odh-notebook-controller/base"
	notebookImagesPath     = deploy.DefaultManifestPath + "/notebook-images/overlays/additional"
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

func (w *Workbenches) IsEnabled() bool {
	return w.Enabled
}

func (w *Workbenches) SetEnabled(enabled bool) {
	w.Enabled = enabled
}

func (w *Workbenches) ReconcileComponent(
	owner metav1.Object,
	cli client.Client,
	scheme *runtime.Scheme,
	enabled bool,
	namespace string,
	logger logr.Logger,
) error {
	// Set default notebooks namespace
	// Create rhods-notebooks namespace in managed platforms
	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}
	if platform != deploy.OpenDataHub {
		err := common.CreateNamespace(cli, "rhods-notebooks")
		if err != nil {
			// no need to log error as it was already logged in createOdhNamespace
			return err
		}
	}
	// Update Default rolebinding
	err = common.UpdatePodSecurityRolebinding(cli, []string{"notebook-controller-service-account"}, namespace)
	if err != nil {
		return err
	}

	// Update image parameters
	if err := deploy.ApplyImageParams(notebookControllerPath, imageParamMap); err != nil {
		logger.Error(err, "Failed to replace image from params.env", "path", notebookControllerPath)
		return err
	}

	err = deploy.DeployManifestsFromPath(owner, cli, ComponentName,
		notebookControllerPath,
		namespace,
		scheme, enabled, logger)
	if err != nil {
		logger.Error(err, "Failed to set Workbench config", "location", notebookControllerPath)
		return err
	}

	// Update image parameters
	if err := deploy.ApplyImageParams(notebookImagesPath, imageParamMap); err != nil {
		logger.Error(err, "Failed to replace image from params.env", "path", notebookImagesPath)
		return err
	}
	err = deploy.DeployManifestsFromPath(owner, cli, ComponentName,
		notebookImagesPath,
		namespace,
		scheme, enabled, logger)
	if err != nil {
		logger.Error(err, "Failed to set Workbench config", "location", notebookImagesPath)
	}
	return err

}

func (in *Workbenches) DeepCopyInto(out *Workbenches) {
	*out = *in
	out.Component = in.Component
}
