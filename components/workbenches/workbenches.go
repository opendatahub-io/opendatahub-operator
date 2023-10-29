// Package workbenches provides utility functions to config Workbenches to secure Jupyter Notebook in Kubernetes environments with support for OAuth
package workbenches

import (
	"path/filepath"
	"strings"

	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ComponentName          = "workbenches"
	DependentComponentName = "notebooks"
	// manifests for nbc in ODH and downstream + downstream use it for imageparams
	notebookControllerPath = deploy.DefaultManifestPath + "/odh-notebook-controller/odh-notebook-controller/base"
	// manifests for ODH nbc
	kfnotebookControllerPath    = deploy.DefaultManifestPath + "/odh-notebook-controller/kf-notebook-controller/overlays/openshift"
	notebookImagesPath          = deploy.DefaultManifestPath + "/notebooks/overlays/additional"
	notebookImagesPathSupported = deploy.DefaultManifestPath + "/jupyterhub/notebooks/base"
)

type Workbenches struct {
	components.Component `json:""`
}

func (w *Workbenches) OverrideManifests(platform string) error {
	// Download manifests if defined by devflags
	if len(w.DevFlags.Manifests) != 0 {
		// Go through each manifests and set the overlays if defined
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
				if platform == string(deploy.ManagedRhods) || platform == string(deploy.SelfManagedRhods) {
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
	}
	return nil
}

func (w *Workbenches) GetComponentName() string {
	return ComponentName
}

// Verifies that Workbench implements ComponentInterface.
var _ components.ComponentInterface = (*Workbenches)(nil)

func (w *Workbenches) ReconcileComponent(cli client.Client, owner metav1.Object, dscispec *dsci.DSCInitializationSpec) error {
	var imageParamMap = map[string]string{
		"odh-notebook-controller-image":    "RELATED_IMAGE_ODH_NOTEBOOK_CONTROLLER_IMAGE",
		"odh-kf-notebook-controller-image": "RELATED_IMAGE_ODH_KF_NOTEBOOK_CONTROLLER_IMAGE",
	}

	// Set default notebooks namespace
	// Create rhods-notebooks namespace in managed platforms
	enabled := w.GetManagementState() == operatorv1.Managed
	monitoringEnabled := dscispec.Monitoring.ManagementState == operatorv1.Managed
	platform, err := deploy.GetPlatform(cli)
	if err != nil {
		return err
	}

	// Set default notebooks namespace
	// Create rhods-notebooks namespace in managed platforms
	if enabled {
		// Download manifests and update paths
		if err = w.OverrideManifests(string(platform)); err != nil {
			return err
		}

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
	}

	if err = deploy.DeployManifestsFromPath(cli, owner, notebookControllerPath, dscispec.ApplicationsNamespace, ComponentName, enabled); err != nil {
		return err
	}

	// Update image parameters for nbc in downstream
	if enabled {
		if dscispec.DevFlags.ManifestsUri == "" && len(w.DevFlags.Manifests) == 0 {
			if platform == deploy.ManagedRhods || platform == deploy.SelfManagedRhods {
				// TODO: fix for nbc new path if want to split into two params.env
				if err := deploy.ApplyParams(notebookControllerPath, w.SetImageParamsMap(imageParamMap), false); err != nil {
					return err
				}
			}
		}
	}
	if err = deploy.DeployManifestsFromPath(cli, owner,
		kfnotebookControllerPath,
		dscispec.ApplicationsNamespace,
		ComponentName, enabled); err != nil {
		return err
	}
	manifestsPath := ""
	if platform == deploy.OpenDataHub || platform == "" {
		manifestsPath = notebookImagesPath
	} else {
		manifestsPath = notebookImagesPathSupported
	}
	if err = deploy.DeployManifestsFromPath(cli, owner,
		manifestsPath,
		dscispec.ApplicationsNamespace,
		ComponentName, enabled); err != nil {
		return err
	}
	// CloudService Monitoring handling
	if platform == deploy.ManagedRhods {
		if err := w.UpdatePrometheusConfig(cli, enabled && monitoringEnabled, ComponentName); err != nil {
			return err
		}
		if err = deploy.DeployManifestsFromPath(cli, owner,
			filepath.Join(deploy.DefaultManifestPath, "monitoring", "prometheus", "apps"),
			dscispec.Monitoring.Namespace,
			ComponentName+"prometheus", true); err != nil {
			return err
		}
	}
	return nil
}

func (w *Workbenches) DeepCopyInto(target *Workbenches) {
	*target = *w
	target.Component = w.Component
}
