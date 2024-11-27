package datasciencepipelines

import (
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentsv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	ArgoWorkflowCRD = "workflows.argoproj.io"
)

type componentHandler struct{}

func init() { //nolint:gochecknoinits
	cr.Add(&componentHandler{})
}

func (s *componentHandler) GetName() string {
	return componentsv1.DataSciencePipelinesComponentName
}

func (s *componentHandler) GetManagementState(dsc *dscv1.DataScienceCluster) string {
	if s == nil || dsc.Spec.Components.DataSciencePipelines.ManagementState == operatorv1.Removed {
		return string(operatorv1.Removed)
	}
	switch dsc.Spec.Components.DataSciencePipelines.ManagementState {
	case operatorv1.Managed:
		return string(dsc.Spec.Components.DataSciencePipelines.ManagementState)
	default: // Force and Unmanaged case for unknown values, we do not support these yet
		return "Unknown"
	}
}

func (s *componentHandler) Init(platform cluster.Platform) error {
	var imageParamMap = map[string]string{
		// v1
		"IMAGES_APISERVER":         "RELATED_IMAGE_ODH_ML_PIPELINES_API_SERVER_IMAGE",
		"IMAGES_ARTIFACT":          "RELATED_IMAGE_ODH_ML_PIPELINES_ARTIFACT_MANAGER_IMAGE",
		"IMAGES_PERSISTENTAGENT":   "RELATED_IMAGE_ODH_ML_PIPELINES_PERSISTENCEAGENT_IMAGE",
		"IMAGES_SCHEDULEDWORKFLOW": "RELATED_IMAGE_ODH_ML_PIPELINES_SCHEDULEDWORKFLOW_IMAGE",
		"IMAGES_CACHE":             "RELATED_IMAGE_ODH_ML_PIPELINES_CACHE_IMAGE",
		"IMAGES_DSPO":              "RELATED_IMAGE_ODH_DATA_SCIENCE_PIPELINES_OPERATOR_CONTROLLER_IMAGE",
		// v2
		"IMAGESV2_ARGO_APISERVER":          "RELATED_IMAGE_ODH_ML_PIPELINES_API_SERVER_V2_IMAGE",
		"IMAGESV2_ARGO_PERSISTENCEAGENT":   "RELATED_IMAGE_ODH_ML_PIPELINES_PERSISTENCEAGENT_V2_IMAGE",
		"IMAGESV2_ARGO_SCHEDULEDWORKFLOW":  "RELATED_IMAGE_ODH_ML_PIPELINES_SCHEDULEDWORKFLOW_V2_IMAGE",
		"IMAGESV2_ARGO_ARGOEXEC":           "RELATED_IMAGE_ODH_DATA_SCIENCE_PIPELINES_ARGO_ARGOEXEC_IMAGE",
		"IMAGESV2_ARGO_WORKFLOWCONTROLLER": "RELATED_IMAGE_ODH_DATA_SCIENCE_PIPELINES_ARGO_WORKFLOWCONTROLLER_IMAGE",
		"V2_DRIVER_IMAGE":                  "RELATED_IMAGE_ODH_ML_PIPELINES_DRIVER_IMAGE",
		"V2_LAUNCHER_IMAGE":                "RELATED_IMAGE_ODH_ML_PIPELINES_LAUNCHER_IMAGE",
		"IMAGESV2_ARGO_MLMDGRPC":           "RELATED_IMAGE_ODH_MLMD_GRPC_SERVER_IMAGE",
	}

	if err := deploy.ApplyParams(defaultPath.String(), imageParamMap); err != nil {
		return fmt.Errorf("failed to update images on path %s: %w", defaultPath.String(), err)
	}

	return nil
}

func (s *componentHandler) NewCRObject(dsc *dscv1.DataScienceCluster) client.Object {
	dataSciencePipelinesAnnotations := make(map[string]string)
	dataSciencePipelinesAnnotations[annotations.ManagementStateAnnotation] = s.GetManagementState(dsc)

	return client.Object(&componentsv1.DataSciencePipelines{
		TypeMeta: metav1.TypeMeta{
			Kind:       componentsv1.DataSciencePipelinesKind,
			APIVersion: componentsv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        componentsv1.DataSciencePipelinesInstanceName,
			Annotations: dataSciencePipelinesAnnotations,
		},
		Spec: componentsv1.DataSciencePipelinesSpec{
			DataSciencePipelinesCommonSpec: dsc.Spec.Components.DataSciencePipelines.DataSciencePipelinesCommonSpec,
		},
	})
}
