// Package datasciencepipelines provides utility functions to config Data Science Pipelines:
// Pipeline solution for end to end MLOps workflows that support the Kubeflow Pipelines SDK and Tekton
package datasciencepipelines

import (
	"context"
	"fmt"
	"path/filepath"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/monitoring"
)

var (
	ComponentName = "data-science-pipelines-operator"
	Path          = deploy.DefaultManifestPath + "/" + ComponentName + "/base"
)

// Verifies that Dashboard implements ComponentInterface.
var _ components.ComponentInterface = (*DataSciencePipelines)(nil)

// DataSciencePipelines struct holds the configuration for the DataSciencePipelines component.
// +kubebuilder:object:generate=true
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

func (d *DataSciencePipelines) ReconcileComponent(ctx context.Context,
	cli client.Client,
	resConf *rest.Config,
	owner metav1.Object,
	dscispec *dsciv1.DSCInitializationSpec,
	_ bool,
) error {
	var imageParamMap = map[string]string{
		"IMAGES_APISERVER":         "RELATED_IMAGE_ODH_ML_PIPELINES_API_SERVER_IMAGE",
		"IMAGES_ARTIFACT":          "RELATED_IMAGE_ODH_ML_PIPELINES_ARTIFACT_MANAGER_IMAGE",
		"IMAGES_PERSISTENTAGENT":   "RELATED_IMAGE_ODH_ML_PIPELINES_PERSISTENCEAGENT_IMAGE",
		"IMAGES_SCHEDULEDWORKFLOW": "RELATED_IMAGE_ODH_ML_PIPELINES_SCHEDULEDWORKFLOW_IMAGE",
		"IMAGES_CACHE":             "RELATED_IMAGE_ODH_ML_PIPELINES_CACHE_IMAGE",
		"IMAGES_DSPO":              "RELATED_IMAGE_ODH_DATA_SCIENCE_PIPELINES_OPERATOR_CONTROLLER_IMAGE",
	}

	enabled := d.GetManagementState() == operatorv1.Managed
	monitoringEnabled := dscispec.Monitoring.ManagementState == operatorv1.Managed

	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}
	if enabled {
		if d.DevFlags != nil {
			// Download manifests and update paths
			if err = d.OverrideManifests(string(platform)); err != nil {
				return err
			}
		}
		// skip check if the dependent operator has beeninstalled, this is done in dashboard
		// Update image parameters only when we do not have customized manifests set
		if (dscispec.DevFlags == nil || dscispec.DevFlags.ManifestsUri == "") && (d.DevFlags == nil || len(d.DevFlags.Manifests) == 0) {
			if err := deploy.ApplyParams(Path, d.SetImageParamsMap(imageParamMap), false); err != nil {
				return err
			}
		}
	}

	if err := deploy.DeployManifestsFromPath(cli, owner, Path, dscispec.ApplicationsNamespace, ComponentName, enabled); err != nil {
		return err
	}
	// CloudService Monitoring handling
	if platform == deploy.ManagedRhods {
		if enabled {
			// first check if the service is up, so prometheus wont fire alerts when it is just startup
			// only 1 replica should be very quick
			if err := monitoring.WaitForDeploymentAvailable(ctx, resConf, ComponentName, dscispec.ApplicationsNamespace, 10, 1); err != nil {
				return fmt.Errorf("deployment for %s is not ready to server: %w", ComponentName, err)
			}
			fmt.Printf("deployment for %s is done, updating monitoring rules\n", ComponentName)
		}

		if err := d.UpdatePrometheusConfig(cli, enabled && monitoringEnabled, ComponentName); err != nil {
			return err
		}
		if err = deploy.DeployManifestsFromPath(cli, owner,
			filepath.Join(deploy.DefaultManifestPath, "monitoring", "prometheus", "apps"),
			dscispec.Monitoring.Namespace,
			"prometheus", true); err != nil {
			return err
		}
	}
	return nil
}
