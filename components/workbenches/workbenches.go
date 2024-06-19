// Package workbenches provides utility functions to config Workbenches to secure Jupyter Notebook in Kubernetes environments with support for OAuth
// +groupName=datasciencecluster.opendatahub.io
package workbenches

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

var (
	ComponentName          = "workbenches"
	DependentComponentName = "notebooks"
	// manifests for nbc in ODH and downstream + downstream use it for imageparams.
	notebookControllerPath = deploy.DefaultManifestPath + "/odh-notebook-controller/odh-notebook-controller/base"
	// manifests for ODH nbc + downstream use it for imageparams.
	kfnotebookControllerPath    = deploy.DefaultManifestPath + "/odh-notebook-controller/kf-notebook-controller/overlays/openshift"
	notebookImagesPath          = deploy.DefaultManifestPath + "/notebooks/overlays/additional"
	notebookImagesPathSupported = deploy.DefaultManifestPath + "/jupyterhub/notebooks/base"
)

// Verifies that Workbench implements ComponentInterface.
var _ components.ComponentInterface = (*Workbenches)(nil)

// Workbenches struct holds the configuration for the Workbenches component.
// +kubebuilder:object:generate=true
type Workbenches struct {
	components.Component `json:""`
}

func (w *Workbenches) OverrideManifests(platform string) error {
	// Download manifests if defined by devflags
	// Go through each manifest and set the overlays if defined
	for _, subcomponent := range w.DevFlags.Manifests {
		if strings.Contains(subcomponent.URI, DependentComponentName) {
			// Download subcomponent
			if err := deploy.DownloadManifests(DependentComponentName, subcomponent); err != nil {
				return err
			}
			// If overlay is defined, update paths
			defaultKustomizePath := "overlays/additional"
			defaultKustomizePathSupported := "notebook-images/overlays/additional"
			if subcomponent.SourcePath != "" {
				defaultKustomizePath = subcomponent.SourcePath
				defaultKustomizePathSupported = subcomponent.SourcePath
			}
			if platform == string(cluster.ManagedRhods) || platform == string(cluster.SelfManagedRhods) {
				notebookImagesPathSupported = filepath.Join(deploy.DefaultManifestPath, "jupyterhub", defaultKustomizePathSupported)
			} else {
				notebookImagesPath = filepath.Join(deploy.DefaultManifestPath, DependentComponentName, defaultKustomizePath)
			}
		}

		if strings.Contains(subcomponent.ContextDir, "components/odh-notebook-controller") {
			// Download subcomponent
			if err := deploy.DownloadManifests("odh-notebook-controller/odh-notebook-controller", subcomponent); err != nil {
				return err
			}
			// If overlay is defined, update paths
			defaultKustomizePathNbc := "base"
			if subcomponent.SourcePath != "" {
				defaultKustomizePathNbc = subcomponent.SourcePath
			}
			notebookControllerPath = filepath.Join(deploy.DefaultManifestPath, "odh-notebook-controller/odh-notebook-controller", defaultKustomizePathNbc)
		}

		if strings.Contains(subcomponent.ContextDir, "components/notebook-controller") {
			// Download subcomponent
			if err := deploy.DownloadManifests("odh-notebook-controller/kf-notebook-controller", subcomponent); err != nil {
				return err
			}
			// If overlay is defined, update paths
			defaultKustomizePathKfNbc := "overlays/openshift"
			if subcomponent.SourcePath != "" {
				defaultKustomizePathKfNbc = subcomponent.SourcePath
			}
			kfnotebookControllerPath = filepath.Join(deploy.DefaultManifestPath, "odh-notebook-controller/kf-notebook-controller", defaultKustomizePathKfNbc)
		}
	}
	return nil
}

func (w *Workbenches) GetComponentName() string {
	return ComponentName
}

func (w *Workbenches) ReconcileComponent(ctx context.Context, cli client.Client, logger logr.Logger,
	owner metav1.Object, dscispec *dsci.DSCInitializationSpec, platform cluster.Platform, _ bool) error {
	l := w.ConfigComponentLogger(logger, ComponentName, dscispec)
	var imageParamMap = map[string]string{
		"odh-notebook-controller-image":    "RELATED_IMAGE_ODH_NOTEBOOK_CONTROLLER_IMAGE",
		"odh-kf-notebook-controller-image": "RELATED_IMAGE_ODH_KF_NOTEBOOK_CONTROLLER_IMAGE",
	}

	// Set default notebooks namespace
	// Create rhods-notebooks namespace in managed platforms
	enabled := w.GetManagementState() == operatorv1.Managed
	monitoringEnabled := dscispec.Monitoring.ManagementState == operatorv1.Managed
	// Set default notebooks namespace
	// Create rhods-notebooks namespace in managed platforms
	if enabled {
		if w.DevFlags != nil {
			// Download manifests and update paths
			if err := w.OverrideManifests(string(platform)); err != nil {
				return err
			}
		}
		if platform == cluster.SelfManagedRhods || platform == cluster.ManagedRhods {
			// Intentionally leaving the ownership unset for this namespace.
			// Specifying this label triggers its deletion when the operator is uninstalled.
			_, err := cluster.CreateNamespace(ctx, cli, "rhods-notebooks", cluster.WithLabels(labels.ODH.OwnedNamespace, "true"))
			if err != nil {
				return err
			}
		}
		// Update Default rolebinding
		err := cluster.UpdatePodSecurityRolebinding(ctx, cli, dscispec.ApplicationsNamespace, "notebook-controller-service-account")
		if err != nil {
			return err
		}
	}
	if err := deploy.DeployManifestsFromPath(cli, owner, notebookControllerPath, dscispec.ApplicationsNamespace, ComponentName, enabled); err != nil {
		return fmt.Errorf("failed to apply manifetss %s: %w", notebookControllerPath, err)
	}
	l.WithValues("Path", notebookControllerPath).Info("apply manifests done NBC")

	// Update image parameters for nbc in downstream
	if enabled {
		if (dscispec.DevFlags == nil || dscispec.DevFlags.ManifestsUri == "") && (w.DevFlags == nil || len(w.DevFlags.Manifests) == 0) {
			if platform == cluster.ManagedRhods || platform == cluster.SelfManagedRhods {
				// for kf-notebook-controller image
				if err := deploy.ApplyParams(notebookControllerPath, imageParamMap, false); err != nil {
					return fmt.Errorf("failed to update image %s: %w", notebookControllerPath, err)
				}
				// for odh-notebook-controller image
				if err := deploy.ApplyParams(kfnotebookControllerPath, imageParamMap, false); err != nil {
					return fmt.Errorf("failed to update image %s: %w", kfnotebookControllerPath, err)
				}
			}
		}
	}
	if err := deploy.DeployManifestsFromPath(cli, owner,
		kfnotebookControllerPath,
		dscispec.ApplicationsNamespace,
		ComponentName, enabled); err != nil {
		return err
	}
	var manifestsPath string
	if platform == cluster.OpenDataHub || platform == "" {
		// only for ODH after transit to kubeflow repo
		if err := deploy.DeployManifestsFromPath(cli, owner,
			kfnotebookControllerPath,
			dscispec.ApplicationsNamespace,
			ComponentName, enabled); err != nil {
			return err
		}
		manifestsPath = notebookImagesPath
	} else {
		manifestsPath = notebookImagesPathSupported
	}
	if err := deploy.DeployManifestsFromPath(cli, owner,
		manifestsPath,
		dscispec.ApplicationsNamespace,
		ComponentName, enabled); err != nil {
		return err
	}
	l.WithValues("Path", manifestsPath).Info("apply manifests done notebook image")
	// CloudService Monitoring handling
	if platform == cluster.ManagedRhods {
		if enabled {
			// first check if the service is up, so prometheus wont fire alerts when it is just startup
			// only 1 replica set timeout to 1min
			if err := cluster.WaitForDeploymentAvailable(ctx, cli, ComponentName, dscispec.ApplicationsNamespace, 10, 1); err != nil {
				return fmt.Errorf("deployments for %s are not ready to server: %w", ComponentName, err)
			}
			l.Info("deployment is done, updating monitoring rules")
		}
		if err := w.UpdatePrometheusConfig(cli, enabled && monitoringEnabled, ComponentName); err != nil {
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
