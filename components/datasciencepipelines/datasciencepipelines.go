// Package datasciencepipelines provides utility functions to config Data Science Pipelines:
// Pipeline solution for end to end MLOps workflows that support the Kubeflow Pipelines SDK, Tekton and Argo Workflows.
// +groupName=datasciencecluster.opendatahub.io
package datasciencepipelines

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/go-logr/logr"
	operatorv1 "github.com/openshift/api/operator/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

var (
	ComponentName   = "data-science-pipelines-operator"
	Path            = deploy.DefaultManifestPath + "/" + ComponentName + "/base"
	OverlayPath     = deploy.DefaultManifestPath + "/" + ComponentName + "/overlays"
	ArgoWorkflowCRD = "workflows.argoproj.io"
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
	logger logr.Logger,
	owner metav1.Object,
	dscispec *dsciv1.DSCInitializationSpec,
	platform cluster.Platform,
	_ bool,
) error {
	l := d.ConfigComponentLogger(logger, ComponentName, dscispec)
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

	enabled := d.GetManagementState() == operatorv1.Managed
	monitoringEnabled := dscispec.Monitoring.ManagementState == operatorv1.Managed

	if enabled {
		if d.DevFlags != nil {
			// Download manifests and update paths
			if err := d.OverrideManifests(string(platform)); err != nil {
				return err
			}
		}
		// skip check if the dependent operator has beeninstalled, this is done in dashboard
		// Update image parameters only when we do not have customized manifests set
		if (dscispec.DevFlags == nil || dscispec.DevFlags.ManifestsUri == "") && (d.DevFlags == nil || len(d.DevFlags.Manifests) == 0) {
			if err := deploy.ApplyParams(Path, imageParamMap, false); err != nil {
				return fmt.Errorf("failed to update image from %s : %w", Path, err)
			}
		}
		// Check for existing Argo Workflows
		if err := UnmanagedArgoWorkFlowExists(ctx, cli); err != nil {
			return err
		}
	}

	// new overlay
	manifestsPath := filepath.Join(OverlayPath, "rhoai")
	if platform == cluster.OpenDataHub || platform == "" {
		manifestsPath = filepath.Join(OverlayPath, "odh")
	}
	if err := deploy.DeployManifestsFromPath(cli, owner, manifestsPath, dscispec.ApplicationsNamespace, ComponentName, enabled); err != nil {
		return err
	}
	l.Info("apply manifests done")

	// CloudService Monitoring handling
	if platform == cluster.ManagedRhods {
		if enabled {
			// first check if the service is up, so prometheus won't fire alerts when it is just startup
			// only 1 replica should be very quick
			if err := cluster.WaitForDeploymentAvailable(ctx, cli, ComponentName, dscispec.ApplicationsNamespace, 10, 1); err != nil {
				return fmt.Errorf("deployment for %s is not ready to server: %w", ComponentName, err)
			}
			l.Info("deployment is done, updating monitoring rules")
		}

		if err := d.UpdatePrometheusConfig(cli, enabled && monitoringEnabled, ComponentName); err != nil {
			return err
		}
		if err := deploy.DeployManifestsFromPath(cli, owner,
			filepath.Join(deploy.DefaultManifestPath, "monitoring", "prometheus", "apps"),
			dscispec.Monitoring.Namespace,
			"prometheus", true); err != nil {
			return err
		}
		l.Info("updating SRE monitoring done")
	}

	return nil
}

func UnmanagedArgoWorkFlowExists(ctx context.Context,
	cli client.Client) error {
	workflowCRD := &apiextensionsv1.CustomResourceDefinition{}
	if err := cli.Get(ctx, client.ObjectKey{Name: ArgoWorkflowCRD}, workflowCRD); err != nil {
		if apierrs.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get existing Workflow CRD : %w", err)
	}
	// Verify if existing workflow is deployed by ODH with label
	odhLabelValue, odhLabelExists := workflowCRD.Labels[labels.ODH.Component(ComponentName)]
	if odhLabelExists && odhLabelValue == "true" {
		return nil
	}
	return fmt.Errorf("%s CRD already exists but not deployed by this operator. "+
		"Remove existing Argo workflows or set `spec.components.datasciencepipelines.managementState` to Removed to proceed ", ArgoWorkflowCRD)
}
