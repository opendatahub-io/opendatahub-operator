package datasciencepipelines

import (
	"github.com/go-logr/logr"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ComponentName = "data-science-pipelines-operator"
	DSPPath       = deploy.DefaultManifestPath + "/" + ComponentName + "/base"
)

var imageParamMap = map[string]string{
	"IMAGES_APISERVER":         "RELATED_IMAGE_ODH_ML_PIPELINES_API_SERVER_IMAGE",
	"IMAGES_ARTIFACT":          "RELATED_IMAGE_ODH_ML_PIPELINES_ARTIFACT_MANAGER_IMAGE",
	"IMAGES_PERSISTENTAGENT":   "RELATED_IMAGE_ODH_ML_PIPELINES_PERSISTENCEAGENT_IMAGE",
	"IMAGES_SCHEDULEDWORKFLOW": "RELATED_IMAGE_ODH_ML_PIPELINES_SCHEDULEDWORKFLOW_IMAGE",
	"IMAGES_CACHE":             "RELATED_IMAGE_ODH_ML_PIPELINES_CACHE_IMAGE",
	"IMAGES_DSPO":              "RELATED_IMAGE_ODH_DATA_SCIENCE_PIPELINES_OPERATOR_CONTROLLER_IMAGE",
}

type DataSciencePipelines struct {
	components.Component `json:""`
}

func (d *DataSciencePipelines) SetImageParamsMap(imageMap map[string]string) map[string]string {
	imageParamMap = imageMap
	return imageParamMap
}

func (d *DataSciencePipelines) GetComponentName() string {
	return ComponentName
}

// Verifies that Dashboard implements ComponentInterface
var _ components.ComponentInterface = (*DataSciencePipelines)(nil)

func (d *DataSciencePipelines) IsEnabled() bool {
	return d.Enabled
}

func (d *DataSciencePipelines) SetEnabled(enabled bool) {
	d.Enabled = enabled
}

func (d *DataSciencePipelines) ReconcileComponent(
	owner metav1.Object,
	client client.Client,
	scheme *runtime.Scheme,
	enabled bool,
	namespace string,
	logger logr.Logger,
) error {

	// Update image parameters
	if err := deploy.ApplyImageParams(DSPPath, imageParamMap); err != nil {
		logger.Error(err, "Failed to replace image from params.env", "path", DSPPath)
		return err
	}

	err := deploy.DeployManifestsFromPath(owner, client, ComponentName,
		DSPPath,
		namespace,
		scheme, enabled, logger)
	if err != nil {
		logger.Error(err, "Failed to set DataSciencePipeline config", "path", DSPPath)
	}
	return err

}

func (in *DataSciencePipelines) DeepCopyInto(out *DataSciencePipelines) {
	*out = *in
	out.Component = in.Component
}
