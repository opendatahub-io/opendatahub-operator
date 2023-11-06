// Package datasciencepipelines provides utility functions to config Data Science Pipelines:
// Pipeline solution for end to end MLOps workflows that support the Kubeflow Pipelines SDK and Tekton
package datasciencepipelines

import (
	"path/filepath"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
)

var (
	ComponentName = "data-science-pipelines-operator"
	Path          = deploy.DefaultManifestPath + "/" + ComponentName + "/base"
)

type DataSciencePipelines struct {
	components.Component `json:""`
}

func (d *DataSciencePipelines) OverrideManifests(_ string) error {
	// If devflags are set, update default manifests path
	if len(d.DevFlags.Manifests) != 0 {
		manifestConfig := d.DevFlags.Manifests[0]
		if err := deploy.DownloadManifests(ComponentName, manifestConfig); err != nil {
			return err
		}
		// If overlay is defined, update paths
		defaultKustomizePath := "base"
		if manifestConfig.SourcePath != "" {
			defaultKustomizePath = manifestConfig.SourcePath
		}
		Path = filepath.Join(deploy.DefaultManifestPath, ComponentName, defaultKustomizePath)
	}
	return nil
}

func (d *DataSciencePipelines) GetComponentName() string {
	return ComponentName
}

// Verifies that Dashboard implements ComponentInterface.
var _ components.ComponentInterface = (*DataSciencePipelines)(nil)

func (d *DataSciencePipelines) ReconcileComponent(cli client.Client, owner metav1.Object, dscispec *dsci.DSCInitializationSpec) error {
	var imageParamMap = map[string]string{
		"IMAGES_APISERVER":         "RELATED_IMAGE_ODH_ML_PIPELINES_API_SERVER_IMAGE",
		"IMAGES_ARTIFACT":          "RELATED_IMAGE_ODH_ML_PIPELINES_ARTIFACT_MANAGER_IMAGE",
		"IMAGES_PERSISTENTAGENT":   "RELATED_IMAGE_ODH_ML_PIPELINES_PERSISTENCEAGENT_IMAGE",
		"IMAGES_SCHEDULEDWORKFLOW": "RELATED_IMAGE_ODH_ML_PIPELINES_SCHEDULEDWORKFLOW_IMAGE",
		"IMAGES_CACHE":             "RELATED_IMAGE_ODH_ML_PIPELINES_CACHE_IMAGE",
		"IMAGES_DSPO":              "RELATED_IMAGE_ODH_DATA_SCIENCE_PIPELINES_OPERATOR_CONTROLLER_IMAGE",
	}

	enabled := d.GetManagementState() == operatorv1.Managed
	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}
	if enabled {
		// Download manifests and update paths
		if err = d.OverrideManifests(string(platform)); err != nil {
			return err
		}

		// check if the dependent operator installed is done in dashboard

		// Update image parameters only when we do not have customized manifests set
		if dscispec.DevFlags.ManifestsUri == "" && len(d.DevFlags.Manifests) == 0 {
			if err := deploy.ApplyParams(Path, d.SetImageParamsMap(imageParamMap), false); err != nil {
				return err
			}
		}
	}

	err = deploy.DeployManifestsFromPath(cli, owner, Path, dscispec.ApplicationsNamespace, d.GetComponentName(), enabled)
	return err
}

func (d *DataSciencePipelines) DeepCopyInto(target *DataSciencePipelines) {
	*target = *d
	target.Component = d.Component
}
